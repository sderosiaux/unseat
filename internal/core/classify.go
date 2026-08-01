package core

import (
	"strings"
)

// SeatClass describes why a SaaS seat exists relative to the corporate directory.
//
// The distinction matters because a single "not in the mapped group" bucket
// conflates a departed employee (delete the seat, save the money) with a
// current employee the mappings simply do not cover yet (touching it would be
// an outage). Only SeatOrphan is ever safe to reclaim automatically.
type SeatClass string

const (
	// SeatManaged: active directory user, inside a group mapped to this provider.
	SeatManaged SeatClass = "managed"
	// SeatUnmapped: active directory user, outside every group mapped to this
	// provider. Report it, never remove it — it usually means incomplete mappings.
	SeatUnmapped SeatClass = "unmapped"
	// SeatOrphan: no active directory identity. This is the money and the risk.
	SeatOrphan SeatClass = "orphan"
	// SeatExternal: identity outside the corporate domain — guest, contractor,
	// personal account. Needs a human decision, not automation.
	SeatExternal SeatClass = "external"
	// SeatUnresolved: the provider exposes a username with no email, and no
	// alias maps it to a person. Cannot be judged — surfaced so it gets fixed.
	SeatUnresolved SeatClass = "unresolved"
)

// DirectoryUser is the minimal identity view needed to classify a seat.
type DirectoryUser struct {
	Email     string
	Suspended bool
}

// ClassifyInput carries everything needed to classify one provider's seats.
type ClassifyInput struct {
	ProviderName string
	// ActualUsers are the seats reported by the SaaS provider.
	ActualUsers []User
	// Directory is every identity known to the identity source, keyed by
	// lowercased email. A nil map means the directory could not be read, and
	// classification degrades to external/unresolved only.
	Directory map[string]DirectoryUser
	// DesiredEmails is the union of members of the groups mapped to this
	// provider, lowercased.
	DesiredEmails map[string]bool
	// Domain is the corporate domain, e.g. "acme.com".
	Domain string
	// AliasIndex maps a lowercased provider username or personal email to a
	// canonical corporate email.
	AliasIndex map[string]string
	// Exceptions are lowercased emails that policy protects from removal.
	Exceptions map[string]bool
}

// ClassifiedSeat is one SaaS seat with its verdict.
type ClassifiedSeat struct {
	Provider string `json:"provider"`
	// Email is the alias-resolved, lowercased identity.
	Email string `json:"email"`
	// RawEmail is what the provider actually reported, kept because it is what
	// an operator sees in the vendor's own admin UI.
	RawEmail  string    `json:"raw_email"`
	User      User      `json:"user"`
	Class     SeatClass `json:"class"`
	Reason    string    `json:"reason"`
	Protected bool      `json:"protected"`
}

// Reclaimable reports whether this seat can be safely released automatically.
func (s ClassifiedSeat) Reclaimable() bool {
	return s.Class == SeatOrphan && !s.Protected
}

func resolveAlias(aliasIndex map[string]string, email string) string {
	key := strings.ToLower(strings.TrimSpace(email))
	if aliasIndex != nil {
		if canonical, ok := aliasIndex[key]; ok {
			return canonical
		}
	}
	return key
}

// ClassifySeats assigns a class to every seat reported by a provider.
func ClassifySeats(input ClassifyInput) []ClassifiedSeat {
	domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input.Domain), "@"))
	seats := make([]ClassifiedSeat, 0, len(input.ActualUsers))

	for _, u := range input.ActualUsers {
		resolved := resolveAlias(input.AliasIndex, u.Email)

		seat := ClassifiedSeat{
			Provider:  input.ProviderName,
			Email:     resolved,
			RawEmail:  u.Email,
			User:      u,
			Protected: input.Exceptions[resolved],
		}

		switch {
		case !strings.Contains(resolved, "@"):
			seat.Class = SeatUnresolved
			seat.Reason = "provider reports a username with no email, and no alias maps it to a person"

		case domain != "" && !strings.HasSuffix(resolved, "@"+domain):
			seat.Class = SeatExternal
			seat.Reason = "identity outside the corporate domain " + domain

		case input.Directory == nil:
			seat.Class = SeatUnresolved
			seat.Reason = "directory unavailable — cannot determine employment status"

		default:
			dir, known := input.Directory[resolved]
			switch {
			case !known:
				seat.Class = SeatOrphan
				seat.Reason = "no matching identity in the directory"
			case dir.Suspended:
				seat.Class = SeatOrphan
				seat.Reason = "directory identity is suspended"
			case input.DesiredEmails[resolved]:
				seat.Class = SeatManaged
				seat.Reason = "member of a group mapped to this provider"
			default:
				seat.Class = SeatUnmapped
				seat.Reason = "active employee, but no mapped group grants this provider"
			}
		}

		seats = append(seats, seat)
	}

	return seats
}

// ClassSummary counts seats per class for a provider.
type ClassSummary struct {
	Provider   string `json:"provider"`
	Managed    int    `json:"managed"`
	Unmapped   int    `json:"unmapped"`
	Orphan     int    `json:"orphan"`
	External   int    `json:"external"`
	Unresolved int    `json:"unresolved"`
	Protected  int    `json:"protected"`
	Total      int    `json:"total"`
}

// SummarizeSeats aggregates classified seats into per-class counts.
func SummarizeSeats(provider string, seats []ClassifiedSeat) ClassSummary {
	s := ClassSummary{Provider: provider, Total: len(seats)}
	for _, seat := range seats {
		if seat.Protected {
			s.Protected++
		}
		switch seat.Class {
		case SeatManaged:
			s.Managed++
		case SeatUnmapped:
			s.Unmapped++
		case SeatOrphan:
			s.Orphan++
		case SeatExternal:
			s.External++
		case SeatUnresolved:
			s.Unresolved++
		}
	}
	return s
}
