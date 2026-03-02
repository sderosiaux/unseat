package google

import (
	"context"
	"fmt"
	"os"

	"github.com/sderosiaux/unseat/internal/core"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
	"golang.org/x/oauth2/google"
)

type Provider struct {
	service *admin.Service
	domain  string
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
		conf, jwtErr := google.JWTConfigFromJSON(data,
			admin.AdminDirectoryUserReadonlyScope,
			admin.AdminDirectoryGroupReadonlyScope,
		)
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
	return &Provider{service: svc, domain: domain}, nil
}

type options struct {
	adminEmail string
}

type Option func(*options)

func WithAdminEmail(email string) Option {
	return func(o *options) { o.adminEmail = email }
}

func (p *Provider) Name() string { return "google-directory" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     true,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: true,
		HasWebhook: true,
	}
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var users []core.User
	call := p.service.Users.List().Domain(p.domain).MaxResults(500)
	err := call.Pages(ctx, func(resp *admin.Users) error {
		for _, u := range resp.Users {
			status := "active"
			if u.Suspended {
				status = "suspended"
			}
			users = append(users, core.User{
				Email:       u.PrimaryEmail,
				DisplayName: u.Name.FullName,
				Role:        boolToRole(u.IsAdmin),
				Status:      status,
				ProviderID:  u.Id,
			})
		}
		return nil
	})
	return users, err
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("google-directory: AddUser not supported — manage users via Google Workspace admin")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	return p.service.Users.Delete(email).Context(ctx).Do()
}

func (p *Provider) SetRole(ctx context.Context, email, role string) error {
	_, err := p.service.Users.Update(email, &admin.User{
		IsAdmin: role == "admin",
	}).Context(ctx).Do()
	return err
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
	call := p.service.Members.List(groupEmail).MaxResults(200)
	err := call.Pages(ctx, func(resp *admin.Members) error {
		for _, m := range resp.Members {
			if m.Type != "USER" {
				continue
			}
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

func boolToRole(isAdmin bool) string {
	if isAdmin {
		return "admin"
	}
	return "member"
}
