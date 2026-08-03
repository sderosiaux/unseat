package core

import (
	"sort"
	"strings"
)

type IdentityLinkType string

const (
	IdentityLinkDirectoryPrimary IdentityLinkType = "directory_primary"
	IdentityLinkDirectoryAlias   IdentityLinkType = "directory_alias"
	IdentityLinkExplicitMapping  IdentityLinkType = "explicit_mapping"
	IdentityLinkProviderEmail    IdentityLinkType = "provider_verified_email"
	IdentityLinkProviderUsername IdentityLinkType = "provider_username_match"
	IdentityLinkPersonalEmail    IdentityLinkType = "personal_email_match"
)

type IdentityConfidence string

const (
	IdentityConfidenceStrong  IdentityConfidence = "strong"
	IdentityConfidenceWeak    IdentityConfidence = "weak"
	IdentityConfidenceUnknown IdentityConfidence = "unknown"
)

type IdentityResolutionStatus string

const (
	IdentityMatched   IdentityResolutionStatus = "matched"
	IdentityUnmatched IdentityResolutionStatus = "unmatched"
	IdentityAmbiguous IdentityResolutionStatus = "ambiguous"
)

type IdentityLink struct {
	RawIdentifier  string                   `json:"raw_identifier"`
	CanonicalEmail string                   `json:"canonical_email,omitempty"`
	Type           IdentityLinkType         `json:"type"`
	Confidence     IdentityConfidence       `json:"confidence"`
	Status         IdentityResolutionStatus `json:"status"`
	Reason         string                   `json:"reason,omitempty"`
	EvidenceIDs    []string                 `json:"evidence_ids,omitempty"`
}

type CanonicalIdentity struct {
	PrimaryEmail     string                   `json:"primary_email"`
	DirectoryStatus  string                   `json:"directory_status,omitempty"`
	EmploymentSource string                   `json:"employment_source,omitempty"`
	Aliases          []string                 `json:"aliases,omitempty"`
	Confidence       IdentityConfidence       `json:"confidence"`
	Status           IdentityResolutionStatus `json:"status"`
	Links            []IdentityLink           `json:"links,omitempty"`
}

type ResolveIdentityInput struct {
	Subject       string
	Directory     []User
	Aliases       map[string][]string
	ProviderUsers map[string][]User
}

// ResolveCanonicalIdentity turns a user-supplied identifier into a canonical
// identity plus the evidence-quality of every match it can make.
func ResolveCanonicalIdentity(in ResolveIdentityInput) CanonicalIdentity {
	subject := strings.ToLower(strings.TrimSpace(in.Subject))
	aliasToCanonical := explicitAliasIndex(in.Aliases)
	directory := make(map[string]User, len(in.Directory))
	for _, u := range in.Directory {
		directory[strings.ToLower(strings.TrimSpace(u.Email))] = u
	}

	primary := subject
	var links []IdentityLink

	if canonical, ok := aliasToCanonical[subject]; ok {
		primary = canonical
		links = append(links, IdentityLink{
			RawIdentifier:  subject,
			CanonicalEmail: canonical,
			Type:           IdentityLinkExplicitMapping,
			Confidence:     IdentityConfidenceStrong,
			Status:         IdentityMatched,
			Reason:         "subject matched an explicit alias",
		})
	}

	if _, ok := directory[primary]; ok {
		links = append(links, IdentityLink{
			RawIdentifier:  primary,
			CanonicalEmail: primary,
			Type:           IdentityLinkDirectoryPrimary,
			Confidence:     IdentityConfidenceStrong,
			Status:         IdentityMatched,
			Reason:         "identity source reports this primary email",
		})
	}

	knownEmails := make([]string, 0, len(directory))
	for email := range directory {
		knownEmails = append(knownEmails, email)
	}
	aliasIndex := BuildAliasIndex(in.Aliases, knownEmails)

	var weakMatches int
	for providerName, users := range in.ProviderUsers {
		for _, u := range users {
			link, ok := resolveProviderUserLink(providerName, u, primary, aliasIndex)
			if !ok {
				continue
			}
			if link.Confidence == IdentityConfidenceWeak {
				weakMatches++
			}
			links = append(links, link)
		}
	}

	status := IdentityUnmatched
	confidence := IdentityConfidenceUnknown
	if len(links) > 0 {
		status = IdentityMatched
		confidence = IdentityConfidenceStrong
	}
	if weakMatches > 0 && len(strongLinks(links)) == 0 {
		status = IdentityAmbiguous
		confidence = IdentityConfidenceWeak
	}

	dirStatus := ""
	if u, ok := directory[primary]; ok {
		dirStatus = u.Status
	}

	aliases := append([]string{}, in.Aliases[primary]...)
	sort.Strings(aliases)
	sortIdentityLinks(links)

	return CanonicalIdentity{
		PrimaryEmail:     primary,
		DirectoryStatus:  dirStatus,
		EmploymentSource: "directory",
		Aliases:          aliases,
		Confidence:       confidence,
		Status:           status,
		Links:            links,
	}
}

func explicitAliasIndex(aliases map[string][]string) map[string]string {
	out := make(map[string]string)
	for canonical, values := range aliases {
		canonical = strings.ToLower(strings.TrimSpace(canonical))
		for _, alias := range values {
			out[strings.ToLower(strings.TrimSpace(alias))] = canonical
		}
	}
	return out
}

func resolveProviderUserLink(providerName string, u User, primary string, aliasIndex map[string]string) (IdentityLink, bool) {
	raw := strings.ToLower(strings.TrimSpace(u.Email))
	if raw == "" {
		return IdentityLink{}, false
	}
	resolved := resolveAlias(aliasIndex, raw)
	if resolved == primary {
		linkType := IdentityLinkProviderEmail
		confidence := IdentityConfidenceStrong
		reason := "provider reported the canonical email"
		if raw != primary {
			linkType = IdentityLinkExplicitMapping
			reason = "provider identifier matched an alias"
		}
		if !strings.Contains(raw, "@") && raw != primary {
			linkType = IdentityLinkProviderUsername
			if _, explicit := aliasIndex[raw]; explicit {
				confidence = IdentityConfidenceStrong
				reason = "provider username matched an explicit or generated alias"
			} else {
				confidence = IdentityConfidenceWeak
				reason = "provider username resembles the identity"
			}
		}
		return IdentityLink{
			RawIdentifier:  raw,
			CanonicalEmail: primary,
			Type:           linkType,
			Confidence:     confidence,
			Status:         IdentityMatched,
			Reason:         providerName + ": " + reason,
		}, true
	}

	if strings.Contains(raw, "@") && localPart(raw) == localPart(primary) {
		return IdentityLink{
			RawIdentifier:  raw,
			CanonicalEmail: primary,
			Type:           IdentityLinkPersonalEmail,
			Confidence:     IdentityConfidenceWeak,
			Status:         IdentityAmbiguous,
			Reason:         providerName + ": external address local part matches the subject; requires confirmation",
		}, true
	}

	return IdentityLink{}, false
}

func strongLinks(links []IdentityLink) []IdentityLink {
	var out []IdentityLink
	for _, link := range links {
		if link.Confidence == IdentityConfidenceStrong {
			out = append(out, link)
		}
	}
	return out
}

func sortIdentityLinks(links []IdentityLink) {
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Confidence != links[j].Confidence {
			return links[i].Confidence < links[j].Confidence
		}
		if links[i].Type != links[j].Type {
			return links[i].Type < links[j].Type
		}
		return links[i].RawIdentifier < links[j].RawIdentifier
	})
}

func (id CanonicalIdentity) Matches(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}
	if raw == id.PrimaryEmail {
		return true
	}
	for _, alias := range id.Aliases {
		if strings.EqualFold(raw, alias) {
			return true
		}
	}
	for _, link := range id.Links {
		if link.Status == IdentityMatched && strings.EqualFold(raw, link.RawIdentifier) {
			return true
		}
	}
	return false
}

func (id CanonicalIdentity) AmbiguousMatch(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}
	for _, link := range id.Links {
		if link.Status == IdentityAmbiguous && strings.EqualFold(raw, link.RawIdentifier) {
			return true
		}
	}
	return false
}
