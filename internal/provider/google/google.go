package google

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"golang.org/x/oauth2/google"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
)

// delegatedScopes are the scopes requested when impersonating an admin through
// domain-wide delegation. They must be write scopes: the provider suspends
// users and toggles admin status, and the readonly variants 403 on every write.
var delegatedScopes = []string{
	admin.AdminDirectoryUserScope,
	admin.AdminDirectoryGroupScope,
}

type Provider struct {
	service    *admin.Service
	domain     string
	hardDelete bool
}

func New(ctx context.Context, credentialsFile, domain string, opts ...Option) (*Provider, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	var svc *admin.Service
	var err error

	if o.adminEmail != "" {
		// Domain-wide delegation: impersonate an admin user.
		data, readErr := os.ReadFile(credentialsFile)
		if readErr != nil {
			return nil, fmt.Errorf("read credentials: %w", readErr)
		}
		conf, jwtErr := google.JWTConfigFromJSON(data, delegatedScopes...)
		if jwtErr != nil {
			return nil, fmt.Errorf("parse credentials: %w", jwtErr)
		}
		conf.Subject = o.adminEmail
		client := conf.Client(ctx)
		svc, err = admin.NewService(ctx, option.WithHTTPClient(client))
	} else {
		svc, err = admin.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	}

	if err != nil {
		return nil, fmt.Errorf("create admin service: %w", err)
	}
	return &Provider{service: svc, domain: domain, hardDelete: o.hardDelete}, nil
}

type options struct {
	adminEmail string
	hardDelete bool
}

type Option func(*options)

func WithAdminEmail(email string) Option {
	return func(o *options) { o.adminEmail = email }
}

// WithHardDelete makes RemoveUser permanently delete the Workspace account
// instead of suspending it. Opt-in only: deletion destroys mail and Drive data
// and is unrecoverable after 20 days.
func WithHardDelete(enabled bool) Option {
	return func(o *options) { o.hardDelete = enabled }
}

func (p *Provider) Name() string { return "google-directory" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:          false, // account creation stays in the Workspace admin console
		CanRemove:       true,
		CanSuspend:      true,
		CanSetRole:      true,
		HasWebhook:      true,
		ReportsActivity: true,
	}
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var users []core.User
	call := p.service.Users.List().Domain(p.domain).MaxResults(500)
	err := call.Pages(ctx, func(resp *admin.Users) error {
		for _, u := range resp.Users {
			// Archived accounts cannot sign in — counting them as active would
			// bill them as live seats forever.
			status := "active"
			if u.Suspended || u.Archived {
				status = "suspended"
			}
			users = append(users, core.User{
				Email:          u.PrimaryEmail,
				DisplayName:    u.Name.FullName,
				Role:           boolToRole(u.IsAdmin),
				Status:         status,
				ProviderID:     u.Id,
				LastActivityAt: parseLastLogin(u.LastLoginTime),
			})
		}
		return nil
	})
	return users, err
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("google-directory: AddUser not supported — manage users via Google Workspace admin")
}

// RemoveUser suspends the account. Suspension revokes sign-in and frees the
// licence while keeping the data recoverable; deletion is only reachable with
// WithHardDelete.
func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	if p.hardDelete {
		return p.service.Users.Delete(email).Context(ctx).Do()
	}
	_, err := p.service.Users.Update(email, &admin.User{
		Suspended:       true,
		ForceSendFields: []string{"Suspended"},
	}).Context(ctx).Do()
	return err
}

func (p *Provider) SetRole(ctx context.Context, email, role string) error {
	var status bool
	switch role {
	case "admin":
		status = true
	case "member":
		status = false
	default:
		return fmt.Errorf("google-directory: unsupported role %q — want \"admin\" or \"member\"", role)
	}
	// isAdmin is output-only on users.update: that call returns 200 and changes
	// nothing. makeAdmin is the only endpoint that actually flips it.
	return p.service.Users.MakeAdmin(email, &admin.UserMakeAdmin{
		Status:          status,
		ForceSendFields: []string{"Status"},
	}).Context(ctx).Do()
}

func (p *Provider) ListGroups(ctx context.Context) ([]core.Group, error) {
	var groups []core.Group
	call := p.service.Groups.List().Domain(p.domain).MaxResults(200)
	err := call.Pages(ctx, func(resp *admin.Groups) error {
		for _, g := range resp.Groups {
			groups = append(groups, core.Group{
				ID:          g.Id,
				Email:       g.Email,
				Name:        g.Name,
				Description: g.Description,
				MemberCount: int(g.DirectMembersCount),
			})
		}
		return nil
	})
	return groups, err
}

func (p *Provider) ListGroupMembers(ctx context.Context, groupEmail string) ([]core.User, error) {
	var users []core.User
	seen := make(map[string]bool)
	// IncludeDerivedMembership expands nested groups: without it, someone who is
	// only in a mapped group through a child group looks like a non-member and
	// gets deprovisioned. Derived members come back with type USER, and a user
	// reachable both directly and indirectly is returned twice — hence the dedup.
	call := p.service.Members.List(groupEmail).IncludeDerivedMembership(true).MaxResults(200)
	err := call.Pages(ctx, func(resp *admin.Members) error {
		for _, m := range resp.Members {
			if m.Type != "USER" {
				continue
			}
			key := strings.ToLower(m.Email)
			if seen[key] {
				continue
			}
			seen[key] = true
			users = append(users, core.User{
				Email:      m.Email,
				ProviderID: m.Id,
				Role:       m.Role,
				Status:     m.Status,
			})
		}
		return nil
	})
	return users, err
}

// parseLastLogin converts users.lastLoginTime to a timestamp. Google reports the
// Unix epoch for accounts that have never signed in; that is "unknown", not
// "active in 1970", so it maps to nil.
func parseLastLogin(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || t.Unix() == 0 {
		return nil
	}
	return &t
}

func boolToRole(isAdmin bool) string {
	if isAdmin {
		return "admin"
	}
	return "member"
}
