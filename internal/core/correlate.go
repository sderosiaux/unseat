package core

import (
	"sort"
	"strings"
)

// OffboardingGap is one identity that a provider has deactivated while others
// still carry it as a live, billable seat.
//
// No single provider can see this. A SaaS only knows its own tenant, so a
// half-finished departure is invisible everywhere except from a tool looking at
// all of them at once — which is the whole reason to have one.
type OffboardingGap struct {
	Email string `json:"email"`
	// DeactivatedIn are the providers that consider this identity gone.
	DeactivatedIn []string `json:"deactivated_in"`
	// StillActiveIn are the providers still carrying it as a live seat.
	StillActiveIn []string `json:"still_active_in"`
}

// SeatsByProvider maps a provider name to the users it reported.
type SeatsByProvider map[string][]User

// FindOffboardingGaps reports identities deactivated in at least one provider
// while still active in another.
//
// This is a signal, not a verdict: it does not prove someone left the company,
// only that their access was revoked in one place and not another. That is
// still the cheapest evidence of an incomplete offboarding available without a
// directory, and it needs no configuration beyond the API keys.
func FindOffboardingGaps(seats SeatsByProvider) []OffboardingGap {
	type state struct{ active, deactivated []string }
	byEmail := map[string]*state{}

	for provider, users := range seats {
		for _, u := range users {
			email := strings.ToLower(strings.TrimSpace(u.Email))
			// Usernames with no email cannot be correlated across providers:
			// "jdoe" in one tenant and "jdoe" in another may be different
			// people, and claiming otherwise would name the wrong person.
			if !strings.Contains(email, "@") {
				continue
			}
			s := byEmail[email]
			if s == nil {
				s = &state{}
				byEmail[email] = s
			}
			if u.Status == StatusSuspended {
				s.deactivated = append(s.deactivated, provider)
			} else {
				s.active = append(s.active, provider)
			}
		}
	}

	var gaps []OffboardingGap
	for email, s := range byEmail {
		if len(s.deactivated) == 0 || len(s.active) == 0 {
			continue
		}
		sort.Strings(s.deactivated)
		sort.Strings(s.active)
		gaps = append(gaps, OffboardingGap{
			Email:         email,
			DeactivatedIn: s.deactivated,
			StillActiveIn: s.active,
		})
	}

	// Most exposed first: the more places still granting access, the worse.
	sort.Slice(gaps, func(i, j int) bool {
		if len(gaps[i].StillActiveIn) != len(gaps[j].StillActiveIn) {
			return len(gaps[i].StillActiveIn) > len(gaps[j].StillActiveIn)
		}
		return gaps[i].Email < gaps[j].Email
	})

	return gaps
}
