package offboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
)

// Input is the read-only material needed to produce an offboarding certificate.
type Input struct {
	Config   *config.Config
	Registry *provider.Registry
	Identity provider.IdentityProvider
	Subject  string
	// Providers optionally restricts the run. Empty means every configured
	// provider with credentials.
	Providers []string
	TenantID  string
	Actor     string
	Now       time.Time
}

// RunObserve builds an offboarding certificate without mutating a provider.
func RunObserve(ctx context.Context, in Input) (*core.OffboardingCertificate, error) {
	if in.Config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if in.Registry == nil {
		return nil, fmt.Errorf("provider registry is required")
	}
	if in.Identity == nil {
		return nil, fmt.Errorf("identity provider is required")
	}
	subject := strings.ToLower(strings.TrimSpace(in.Subject))
	if subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	actor := in.Actor
	if actor == "" {
		actor = "system"
	}

	targets := selectedProviders(in.Config, in.Providers)
	directoryUsers, err := in.Identity.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directory users: %w", err)
	}
	directory, knownEmails := directoryIndex(directoryUsers)
	aliasIndex := core.BuildAliasIndex(in.Config.Aliases, knownEmails)
	desired, desiredErrors := desiredByProvider(ctx, in.Config, in.Identity, targets)

	cert := core.OffboardingCertificate{
		ID:          certificateID(subject, now),
		TenantID:    in.TenantID,
		Trigger:     "manual",
		Mode:        core.CertificateModeObserve,
		StartedAt:   now,
		CompletedAt: now,
		Evidence: []core.EvidenceItem{
			evidence(in.Identity.Name(), "ListUsers", core.ObjectUser, "directory_users", in.TenantID, actor, now, directoryUsers, nil),
		},
	}

	inventories := make([]providerInventory, 0, len(targets))
	providerUsers := make(map[string][]core.User, len(targets))
	for _, name := range targets {
		inv := collectProvider(ctx, name, in.Registry, now)
		inventories = append(inventories, inv)
		if inv.userErr == nil {
			providerUsers[name] = inv.users
		}
	}

	cert.Subject = core.ResolveCanonicalIdentity(core.ResolveIdentityInput{
		Subject:       subject,
		Directory:     directoryUsers,
		Aliases:       in.Config.Aliases,
		ProviderUsers: providerUsers,
	})
	if cert.Subject.Status == core.IdentityUnmatched {
		cert.Unknowns = append(cert.Unknowns, "subject was not found in the directory or provider inventories")
	}

	for _, inv := range inventories {
		report := core.ProviderOffboardingReport{
			Provider:     inv.name,
			Capabilities: inv.actions,
		}
		if inv.providerErr != nil {
			report.Errors = append(report.Errors, inv.providerErr.Error())
			cert.Providers = append(cert.Providers, report)
			continue
		}
		if len(desiredErrors[inv.name]) > 0 {
			report.Unknowns = append(report.Unknowns, desiredErrors[inv.name]...)
		}
		if inv.userErr != nil {
			report.Errors = append(report.Errors, "users: "+inv.userErr.Error())
			cert.Providers = append(cert.Providers, report)
			continue
		}

		report.UsersRead = len(inv.users)
		cert.Evidence = append(cert.Evidence,
			evidence(inv.name, "ListUsers", core.ObjectWorkspaceMember, "users", in.TenantID, actor, now, inv.users, nil),
			evidence(inv.name, "Capabilities", core.ObjectUnknownProviderLimit, "capabilities", in.TenantID, actor, now, inv.actions, capabilityLimits(inv.actions)),
		)

		seats := core.ClassifySeats(core.ClassifyInput{
			ProviderName:  inv.name,
			ActualUsers:   inv.users,
			Directory:     directory,
			DesiredEmails: desired[inv.name],
			Domain:        in.Config.CorporateDomain(),
			AliasIndex:    aliasIndex,
			Exceptions:    exceptionsForProvider(in.Config, inv.name),
		})
		subjectSeats := filterSubjectSeats(cert.Subject, seats)
		report.Seats = subjectSeats

		subjectCreds, credentialUnknowns := providerCredentials(cert.Subject, inv, directory, in.Config.CorporateDomain(), aliasIndex)
		report.Credentials = subjectCreds
		report.NonHumanIdentities = nonHumanIdentities(subjectCreds)
		report.Unknowns = append(report.Unknowns, credentialUnknowns...)
		if inv.credentialsSupported && inv.credentialsErr == nil {
			cert.Evidence = append(cert.Evidence, evidence(inv.name, "ListCredentials", core.ObjectCredential, "credentials", in.TenantID, actor, now, inv.credentials, credentialEvidenceLimits(inv.capabilities)))
		}

		billing, billingUnknown := providerBilling(inv, now)
		if billingUnknown != "" {
			report.Unknowns = append(report.Unknowns, billingUnknown)
		}
		if billing != nil {
			cert.Evidence = append(cert.Evidence, evidence(inv.name, "Billing", core.ObjectBillingSeat, "billing", in.TenantID, actor, now, billing, billingEvidenceLimits(billing)))
		}
		claims := core.BuildBillingClaims(core.BillingClaimInput{
			Provider:             inv.name,
			Subject:              cert.Subject.PrimaryEmail,
			Billing:              billing,
			ReclaimableSeatCount: reclaimableSubjectSeats(subjectSeats),
		})
		report.BillingClaims = claims
		for _, claim := range claims {
			if claim.Type == core.BillingClaimMoneyUnknown || claim.Type == core.BillingClaimProcurementRequired {
				report.Unknowns = append(report.Unknowns, claim.Reason)
			}
		}

		decisions := core.BuildDecisions(core.DecisionInput{
			Subject:       cert.Subject,
			Seats:         subjectSeats,
			Credentials:   subjectCreds,
			BillingClaims: claims,
			Capabilities: map[string][]core.ActionCapability{
				inv.name: inv.actions,
			},
		})
		cert.Decisions = append(cert.Decisions, decisions...)
		cert.Providers = append(cert.Providers, report)
		cert.Unknowns = append(cert.Unknowns, providerUnknowns(report, inv.name)...)
	}

	sortCertificate(&cert)
	partitionDecisionViews(&cert)
	cert.Status = core.ComputeCertificateStatus(cert)
	return &cert, nil
}

type providerInventory struct {
	name                 string
	providerErr          error
	userErr              error
	users                []core.User
	capabilities         core.Capabilities
	actions              []core.ActionCapability
	credentialsSupported bool
	credentialsErr       error
	credentialWarnings   []string
	credentials          []core.Credential
	billingSupported     bool
	billingErr           error
	billing              *core.Billing
}

func collectProvider(ctx context.Context, name string, reg *provider.Registry, now time.Time) providerInventory {
	inv := providerInventory{name: name}
	p, err := reg.Get(name)
	if err != nil {
		inv.providerErr = err
		return inv
	}

	inv.capabilities = p.Capabilities()
	inv.actions = inv.capabilities.ActionMatrix(name)
	inv.users, inv.userErr = p.ListUsers(ctx)

	if ip, ok := p.(provider.CredentialInventoryProvider); ok {
		inv.credentialsSupported = true
		inventory, err := ip.ListCredentialInventory(ctx)
		inv.credentials = inventory.Credentials
		inv.credentialWarnings = inventory.Warnings
		inv.credentialsErr = err
	} else if cp, ok := p.(provider.CredentialProvider); ok {
		inv.credentialsSupported = true
		inv.credentials, inv.credentialsErr = cp.ListCredentials(ctx)
	}
	if bp, ok := p.(provider.BillingProvider); ok {
		inv.billingSupported = true
		inv.billing, inv.billingErr = bp.Billing(ctx)
		if inv.billing != nil && inv.billing.FetchedAt.IsZero() {
			inv.billing.FetchedAt = now
		}
	}
	return inv
}

func selectedProviders(cfg *config.Config, requested []string) []string {
	if len(requested) > 0 {
		out := make([]string, 0, len(requested))
		seen := make(map[string]bool, len(requested))
		for _, name := range requested {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
		sort.Strings(out)
		return out
	}
	out := make([]string, 0, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		if pc.APIKey != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func directoryIndex(users []core.User) (map[string]core.DirectoryUser, []string) {
	directory := make(map[string]core.DirectoryUser, len(users))
	knownEmails := make([]string, 0, len(users))
	for _, u := range users {
		key := strings.ToLower(strings.TrimSpace(u.Email))
		if key == "" {
			continue
		}
		directory[key] = core.DirectoryUser{Email: key, Suspended: u.Status == core.StatusSuspended}
		knownEmails = append(knownEmails, key)
	}
	sort.Strings(knownEmails)
	return directory, knownEmails
}

func desiredByProvider(ctx context.Context, cfg *config.Config, identity provider.IdentityProvider, targets []string) (map[string]map[string]bool, map[string][]string) {
	targetSet := make(map[string]bool, len(targets))
	desired := make(map[string]map[string]bool, len(targets))
	for _, name := range targets {
		targetSet[name] = true
		desired[name] = make(map[string]bool)
	}

	errs := make(map[string][]string)
	groupCache := make(map[string][]core.User)
	groupErr := make(map[string]error)
	for _, mapping := range cfg.Mappings {
		for _, pm := range mapping.Providers {
			if !targetSet[pm.Name] {
				continue
			}
			members, ok := groupCache[mapping.Group]
			if !ok {
				var err error
				members, err = identity.ListGroupMembers(ctx, mapping.Group)
				groupCache[mapping.Group] = members
				groupErr[mapping.Group] = err
			}
			if groupErr[mapping.Group] != nil {
				errs[pm.Name] = append(errs[pm.Name], "mapped group "+mapping.Group+": "+groupErr[mapping.Group].Error())
				continue
			}
			for _, u := range members {
				email := strings.ToLower(strings.TrimSpace(u.Email))
				if email != "" {
					desired[pm.Name][email] = true
				}
			}
		}
	}
	return desired, errs
}

func exceptionsForProvider(cfg *config.Config, providerName string) map[string]bool {
	out := make(map[string]bool)
	for _, ex := range cfg.Policies.Exceptions {
		if !exceptionApplies(ex, providerName) {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(ex.Email))
		if email != "" {
			out[email] = true
		}
	}
	return out
}

func exceptionApplies(ex config.Exception, providerName string) bool {
	for _, name := range ex.Providers {
		if name == "*" || name == providerName {
			return true
		}
	}
	return false
}

func filterSubjectSeats(subject core.CanonicalIdentity, seats []core.ClassifiedSeat) []core.ClassifiedSeat {
	out := make([]core.ClassifiedSeat, 0)
	for _, seat := range seats {
		if subject.Matches(seat.Email) || subject.Matches(seat.RawEmail) ||
			subject.AmbiguousMatch(seat.Email) || subject.AmbiguousMatch(seat.RawEmail) {
			out = append(out, seat)
		}
	}
	return out
}

func providerCredentials(subject core.CanonicalIdentity, inv providerInventory, directory map[string]core.DirectoryUser, domain string, aliasIndex map[string]string) ([]core.ClassifiedCredential, []string) {
	if !inv.credentialsSupported {
		return nil, []string{"provider connector does not expose non-human identity inventory"}
	}
	if inv.credentialsErr != nil {
		return nil, []string{"non-human identity inventory unavailable: " + inv.credentialsErr.Error()}
	}

	classified := core.ClassifyCredentials(core.ClassifyCredentialsInput{
		Credentials: inv.credentials,
		Directory:   directory,
		Domain:      domain,
		AliasIndex:  aliasIndex,
	})
	out := make([]core.ClassifiedCredential, 0)
	var unowned, ambiguous int
	for _, cred := range classified {
		owner := cred.Credential.CreatedBy
		switch {
		case owner == "" && cred.Class == core.CredentialUnowned:
			unowned++
		case subject.Matches(owner):
			out = append(out, cred)
		case subject.AmbiguousMatch(owner):
			ambiguous++
			out = append(out, cred)
		}
	}

	var unknowns []string
	unknowns = append(unknowns, inv.credentialWarnings...)
	if unowned > 0 {
		unknowns = append(unknowns, fmt.Sprintf("%d non-human identity credential(s) have no provider-reported owner", unowned))
	}
	if ambiguous > 0 {
		unknowns = append(unknowns, fmt.Sprintf("%d non-human identity credential(s) have ambiguous ownership for this subject", ambiguous))
	}
	if !inv.capabilities.ReportsCredentialUsage && len(classified) > 0 {
		unknowns = append(unknowns, "provider does not report credential last-use timestamps")
	}
	return out, unknowns
}

func nonHumanIdentities(creds []core.ClassifiedCredential) []core.NonHumanIdentity {
	out := make([]core.NonHumanIdentity, 0, len(creds))
	for _, cred := range creds {
		out = append(out, core.CredentialToNHI(cred))
	}
	return out
}

func providerBilling(inv providerInventory, now time.Time) (*core.Billing, string) {
	if !inv.billingSupported {
		return nil, "provider connector does not expose billing API data"
	}
	if inv.billingErr != nil {
		return &core.Billing{
			Provider:          inv.name,
			FetchedAt:         now,
			Source:            core.BillingSourceUnavailable,
			Confidence:        core.BillingConfidenceUnavailable,
			UnavailableReason: "billing API unavailable or missing scope: " + inv.billingErr.Error(),
		}, "billing API unavailable or missing scope: " + inv.billingErr.Error()
	}
	if inv.billing == nil {
		return &core.Billing{
			Provider:          inv.name,
			FetchedAt:         now,
			Source:            core.BillingSourceUnavailable,
			Confidence:        core.BillingConfidenceUnavailable,
			UnavailableReason: "provider billing API returned no subscription data",
		}, "provider billing API returned no subscription data"
	}
	return inv.billing, ""
}

func reclaimableSubjectSeats(seats []core.ClassifiedSeat) int {
	var count int
	for _, seat := range seats {
		if seat.Reclaimable() {
			count++
		}
	}
	return count
}

func providerUnknowns(report core.ProviderOffboardingReport, providerName string) []string {
	out := make([]string, 0, len(report.Unknowns))
	for _, u := range report.Unknowns {
		if u == "" {
			continue
		}
		out = append(out, providerName+": "+u)
	}
	return out
}

func evidence(providerName, endpoint string, kind core.ObjectKind, objectID, tenantID, actor string, at time.Time, payload any, limits []string) core.EvidenceItem {
	ev := core.NewEvidenceItem(providerName, kind, objectID, at, payload)
	ev.TenantID = tenantID
	ev.Actor = actor
	ev.SourceEndpoint = endpoint
	ev.KnownLimits = limits
	return ev
}

func capabilityLimits(actions []core.ActionCapability) []string {
	seen := make(map[string]bool)
	var out []string
	for _, action := range actions {
		for _, limit := range action.KnownLimits {
			if limit == "" || seen[limit] {
				continue
			}
			seen[limit] = true
			out = append(out, limit)
		}
	}
	sort.Strings(out)
	return out
}

func credentialEvidenceLimits(caps core.Capabilities) []string {
	if caps.ReportsCredentialUsage {
		return nil
	}
	return []string{"credential inventory does not prove last-use timestamps"}
}

func billingEvidenceLimits(b *core.Billing) []string {
	if b == nil || b.HasMoney() {
		return nil
	}
	reason := b.UnavailableReason
	if reason == "" {
		reason = "provider API did not expose price"
	}
	return []string{reason}
}

func partitionDecisionViews(cert *core.OffboardingCertificate) {
	for _, decision := range cert.Decisions {
		switch {
		case decision.Status == core.DecisionVerified:
			if decision.ActionClass == core.ActionTransferOwnership || decision.ActionClass == core.ActionRequestOwnerAttestation {
				cert.TransferredResponsibility = append(cert.TransferredResponsibility, decision)
			} else {
				cert.ClosedAccess = append(cert.ClosedAccess, decision)
			}
		case decision.Status == core.DecisionBlocked || decision.ActionClass == core.ActionMarkUnsupported:
			cert.UnsupportedActions = append(cert.UnsupportedActions, decision)
		default:
			cert.PendingReviews = append(cert.PendingReviews, decision)
		}
	}
}

func sortCertificate(cert *core.OffboardingCertificate) {
	sort.SliceStable(cert.Providers, func(i, j int) bool {
		return cert.Providers[i].Provider < cert.Providers[j].Provider
	})
	sort.SliceStable(cert.Decisions, func(i, j int) bool {
		if cert.Decisions[i].Provider != cert.Decisions[j].Provider {
			return cert.Decisions[i].Provider < cert.Decisions[j].Provider
		}
		return cert.Decisions[i].ID < cert.Decisions[j].ID
	})
	sort.SliceStable(cert.Evidence, func(i, j int) bool {
		if cert.Evidence[i].SourceProvider != cert.Evidence[j].SourceProvider {
			return cert.Evidence[i].SourceProvider < cert.Evidence[j].SourceProvider
		}
		if cert.Evidence[i].SourceEndpoint != cert.Evidence[j].SourceEndpoint {
			return cert.Evidence[i].SourceEndpoint < cert.Evidence[j].SourceEndpoint
		}
		return cert.Evidence[i].ObjectID < cert.Evidence[j].ObjectID
	})
	sort.Strings(cert.Unknowns)
}

func certificateID(subject string, startedAt time.Time) string {
	sum := sha256.Sum256([]byte(strings.ToLower(subject) + ":" + startedAt.UTC().Format(time.RFC3339Nano)))
	return "cert_" + hex.EncodeToString(sum[:])[:16]
}
