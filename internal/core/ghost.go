package core

import (
	"sort"
	"strings"
)

// GhostIdentity is a seat classified SeatExternal that, unlike an ordinary
// contractor or guest, actually belongs to a current employee — just under an
// identity the directory does not control.
//
// SeatExternal alone cannot tell the two apart: no single provider can see
// that "tessaljvandermeer@gmail.com" and "tvandermeer@corp.io" are the same
// person. That is exactly what makes it dangerous — deactivating the
// Workspace account does nothing to this seat.
type GhostIdentity struct {
	Provider string `json:"provider"`
	// Identifier is what the provider reports — the personal address or
	// handle actually holding the seat.
	Identifier  string `json:"identifier"`
	DisplayName string `json:"display_name"`
	// Email is the ACTIVE directory identity this seat was matched to. It is
	// what offboarding will deactivate — and, without this report, believe it
	// already covers this seat.
	Email string `json:"email"`
	// Basis records how the match was reached, so a weak one is visible as such.
	Basis string `json:"basis"`
}

// localPart returns the portion of an address before "@", or "" if there is
// none. SeatExternal seats always carry an "@" — classification only reaches
// SeatExternal once an email shape is confirmed — but this stays defensive
// rather than assuming it.
func localPart(email string) string {
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return ""
}

// FindGhostIdentities finds SeatExternal seats that actually belong to a
// current employee, reusing the same name/handle matching AttributeUnresolved
// uses for unresolved seats — the risk lives in the same ambiguity rule: a
// form two identities could claim names neither.
//
// Only seats matched to a directory identity that is currently ACTIVE are
// reported. A departed person who happened to sign up under a personal
// address is an ordinary external — nobody's offboarding is silently
// incomplete because of them.
func FindGhostIdentities(seats []ClassifiedSeat, directory []User) []GhostIdentity {
	keys := buildDirectoryKeys(directory)

	var out []GhostIdentity
	for _, seat := range seats {
		if seat.Class != SeatExternal {
			continue
		}

		name := strings.TrimSpace(seat.User.DisplayName)
		hasName := name != "" && !strings.EqualFold(name, seat.RawEmail)

		var (
			match User
			basis string
			ok    bool
		)

		// The provider's own display name is the strongest signal, tried
		// first exactly as AttributeUnresolved does.
		if hasName {
			match, basis, ok = matchAgainst(nameKey(name), keys)
			if ok {
				basis = "reported name " + basis
			} else {
				// A stated name matching nobody VETOES the weaker branches.
				// Falling through would let the address local part claim a
				// different person: a seat named "Jane Smith" would be
				// attributed to John Smith because "jsmith" matches him. The
				// provider told us who this is and the directory disagreed —
				// that is evidence against a match, not an absence of it.
				continue
			}
		}

		// Next, the local part of the personal address itself: it is often
		// the name in disguise, e.g. tessaljvandermeer for Tessa Vandermeer.
		local := localPart(seat.RawEmail)
		if !ok && local != "" {
			match, basis, ok = matchHandle(local, keys, handleNoise)
			if ok {
				basis = "local part of external address " + basis
			}
		}

		// Finally, a platform-assigned handle if the provider exposes one
		// (GitHub's login, distinct from the email a person signed up with).
		login := seat.User.Metadata["login"]
		if !ok && login != "" && !strings.EqualFold(login, local) {
			match, basis, ok = matchHandle(login, keys, handleNoise)
			if ok {
				basis = "handle " + basis
			}
		}

		if !ok || match.Status == StatusSuspended {
			continue
		}

		out = append(out, GhostIdentity{
			Provider:    seat.Provider,
			Identifier:  seat.RawEmail,
			DisplayName: name,
			Email:       strings.ToLower(match.Email),
			Basis:       basis,
		})
	}

	sortGhosts(out)
	return out
}

func sortGhosts(g []GhostIdentity) {
	sort.Slice(g, func(i, j int) bool {
		if g[i].Email != g[j].Email {
			return g[i].Email < g[j].Email
		}
		return g[i].Identifier < g[j].Identifier
	})
}
