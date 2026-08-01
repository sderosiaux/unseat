package core

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// AliasSuggestion is a proposed link between a provider identifier that could
// not be attributed and a directory identity.
//
// It is a proposal, never applied automatically. Attribution decides whose
// access gets revoked, so the last step stays a human one.
type AliasSuggestion struct {
	Provider string `json:"provider"`
	// Identifier is what the provider reports, e.g. a GitHub login.
	Identifier string `json:"identifier"`
	// DisplayName is the name the provider carries for it, which is what the
	// match was made on.
	DisplayName string `json:"display_name"`
	// Email is the directory identity it appears to be.
	Email string `json:"email"`
	// Basis records how the match was reached, so a weak one is visible as such.
	Basis string `json:"basis"`
}

// stripAccents reduces "Amélie" to "Amelie" so a directory that spells names
// properly still matches a provider that does not.
func stripAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

// nameKey normalises a human name to a comparable form: accents removed,
// lowercased, and everything that is not a letter or digit dropped.
func nameKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(stripAccents(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AttributionReport splits unattributable seats by why they could not be
// attributed, because the reasons call for different actions.
type AttributionReport struct {
	// Matched are seats a directory identity was found for.
	Matched []AliasSuggestion `json:"matched"`
	// NamedButUnknown are seats carrying a real person's name that matches
	// nobody in the directory. That is weak evidence the person has left while
	// keeping the seat — weak because a name can simply be spelled
	// differently, which is why it is reported and not acted on.
	NamedButUnknown []ClassifiedSeat `json:"named_but_unknown"`
	// Anonymous are seats where the provider gave back only a handle, so there
	// is nothing to match on at all.
	Anonymous []ClassifiedSeat `json:"anonymous"`
}

// AttributeUnresolved sorts unattributable seats into what can be proposed,
// what looks like a departure, and what carries no information at all.
func AttributeUnresolved(unresolved []ClassifiedSeat, directory []User) AttributionReport {
	byName := make(map[string][]User, len(directory))
	for _, d := range directory {
		if key := nameKey(d.DisplayName); key != "" {
			byName[key] = append(byName[key], d)
		}
	}

	var rep AttributionReport
	for _, seat := range unresolved {
		name := strings.TrimSpace(seat.User.DisplayName)
		// "No name" means the connector fell back to the handle verbatim.
		// Comparing normalised forms instead would discard real names that
		// merely resemble their handle — "Amélie Brossard" against the login
		// AmelieBrossard — and those are exactly the ones that match.
		if name == "" || strings.EqualFold(name, seat.RawEmail) {
			rep.Anonymous = append(rep.Anonymous, seat)
			continue
		}

		matches := byName[nameKey(name)]
		if len(matches) != 1 {
			rep.NamedButUnknown = append(rep.NamedButUnknown, seat)
			continue
		}

		rep.Matched = append(rep.Matched, AliasSuggestion{
			Provider:    seat.Provider,
			Identifier:  seat.RawEmail,
			DisplayName: name,
			Email:       strings.ToLower(matches[0].Email),
			Basis:       "display name matches exactly one directory identity",
		})
	}

	sort.Slice(rep.Matched, func(i, j int) bool {
		if rep.Matched[i].Email != rep.Matched[j].Email {
			return rep.Matched[i].Email < rep.Matched[j].Email
		}
		return rep.Matched[i].Identifier < rep.Matched[j].Identifier
	})
	sortSeats(rep.NamedButUnknown)
	sortSeats(rep.Anonymous)
	return rep
}

func sortSeats(s []ClassifiedSeat) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Provider != s[j].Provider {
			return s[i].Provider < s[j].Provider
		}
		return strings.ToLower(s[i].RawEmail) < strings.ToLower(s[j].RawEmail)
	})
}

// SuggestAliases returns only the seats a directory identity was found for.
func SuggestAliases(unresolved []ClassifiedSeat, directory []User) []AliasSuggestion {
	return AttributeUnresolved(unresolved, directory).Matched
}
