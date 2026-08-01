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
func AttributeUnresolved(unresolved []ClassifiedSeat, directory []User, domain string) AttributionReport {
	keys := buildDirectoryKeys(directory)
	// People append the company name when the plain handle is taken. It is
	// derived from the configured domain rather than hardcoded.
	noise := handleNoise
	if label := companyLabel(domain); label != "" {
		noise = append(append([]string{}, handleNoise...), "-"+label, "_"+label)
	}

	var rep AttributionReport
	for _, seat := range unresolved {
		name := strings.TrimSpace(seat.User.DisplayName)
		// "No name" means the connector fell back to the handle verbatim.
		// Comparing normalised forms instead would discard real names that
		// merely resemble their handle — "Amélie Brossard" against the login
		// AmelieBrossard — and those are exactly the ones that match.
		hasName := name != "" && !strings.EqualFold(name, seat.RawEmail)

		// The name the provider carries is the stronger signal, so it is tried
		// first — but with the same tolerance as a handle, because providers
		// hold "Raj Iyer" where the directory holds "Rajesh Iyer".
		if hasName {
			if match, basis, ok := matchAgainst(nameKey(name), keys); ok {
				rep.Matched = append(rep.Matched, AliasSuggestion{
					Provider:    seat.Provider,
					Identifier:  seat.RawEmail,
					DisplayName: name,
					Email:       strings.ToLower(match.Email),
					Basis:       "reported name " + basis,
				})
				continue
			}
		}

		// The handle itself is usually the name: harrietlowe,
		// adaokafor-dev, willbenton-acme. Treating a missing name field
		// as "nothing to infer" ignored the identifier sitting in plain sight.
		if match, basis, ok := matchHandle(seat.RawEmail, keys, noise); ok {
			rep.Matched = append(rep.Matched, AliasSuggestion{
				Provider:    seat.Provider,
				Identifier:  seat.RawEmail,
				DisplayName: match.DisplayName,
				Email:       strings.ToLower(match.Email),
				Basis:       "handle " + basis,
			})
			continue
		}

		if hasName {
			rep.NamedButUnknown = append(rep.NamedButUnknown, seat)
			continue
		}
		rep.Anonymous = append(rep.Anonymous, seat)
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

// handleNoise are the decorations people bolt onto a username when the plain
// one is taken: willbenton-acme, adaokafor-dev, someone-collab.
var handleNoise = []string{"-collab", "-dev", "-io", "-inc", "-corp"}

// minPrefix is how much of a name a handle must spell before a prefix counts.
// Below this, short handles collide with unrelated people.
const minPrefix = 6

// minSurname is how long a surname must be to carry a match on its own.
const minSurname = 4

// directoryKey is one identity reduced to the forms a username might take.
type directoryKey struct {
	user User
	// full is the whole name, punctuation and accents removed: marcustremblay.
	full string
	// tokens are the individual name parts: {benjamin, bramble}.
	tokens []string
	// local is the email local part, squashed: willbenton from will.benton@.
	local string
}

func buildDirectoryKeys(directory []User) []directoryKey {
	keys := make([]directoryKey, 0, len(directory))
	for _, u := range directory {
		full := nameKey(u.DisplayName)
		if full == "" {
			continue
		}
		var tokens []string
		for _, t := range strings.FieldsFunc(strings.ToLower(stripAccents(u.DisplayName)), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if t != "" {
				tokens = append(tokens, t)
			}
		}
		local := ""
		if at := strings.IndexByte(strings.ToLower(u.Email), '@'); at > 0 {
			local = nameKey(u.Email[:at])
		}
		keys = append(keys, directoryKey{user: u, full: full, tokens: tokens, local: local})
	}
	return keys
}

// matchesHandle reports whether a normalised handle plausibly spells this
// identity, and how strongly.
//
// Handles are abbreviations of names, not names: Priya Venkatesan signs in as
// priyaven, Rajesh Iyer as raj-acme, William Benton as willbenton.
// Requiring equality attributed none of them.
func (k directoryKey) matchesHandle(h string) (basis string, ok bool) {
	switch {
	case h == k.full:
		return "spells the directory name", true
	case k.local != "" && h == k.local:
		return "spells the directory address", true
	case len(h) >= minPrefix && strings.HasPrefix(k.full, h):
		// priyaven -> priyavenidore
		return "abbreviates the directory name", true
	}

	// Surname in full plus the given name, or the start of it: willbenton for
	// William Benton, benbramble for Benjamin Bramble.
	if len(k.tokens) >= 2 {
		given, surname := k.tokens[0], k.tokens[len(k.tokens)-1]
		if len(surname) >= minSurname && strings.Contains(h, surname) {
			rest := strings.Replace(h, surname, "", 1)
			if rest != "" && (strings.HasPrefix(given, rest) || strings.HasPrefix(rest, given)) {
				return "surname plus given name", true
			}
		}
	}
	return "", false
}

// matchHandle tries to read a directory identity out of the handle itself.
//
// Only exact, unambiguous normalised matches count. Trailing digits and
// company decorations are stripped, but nothing is guessed from a partial
// name: a handle is a weaker signal than a stated name, so it must clear the
// same bar rather than a lower one.
// companyLabel turns acme.com into "acme".
func companyLabel(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	if i := strings.IndexByte(d, '.'); i > 0 {
		d = d[:i]
	}
	return nameKey(d)
}

// matchAgainst finds the single directory identity a normalised string spells,
// or reports that the evidence is not conclusive.
func matchAgainst(form string, keys []directoryKey) (User, string, bool) {
	if form == "" {
		return User{}, "", false
	}
	var found User
	var basis string
	hits := 0
	for _, k := range keys {
		if b, ok := k.matchesHandle(form); ok {
			hits++
			found, basis = k.user, b
		}
	}
	// Exactly one identity may claim a form. Two candidates means the evidence
	// is too weak to name either, and naming the wrong person is what this
	// whole path must not do.
	if hits == 1 {
		return found, basis, true
	}
	return User{}, "", false
}

func matchHandle(handle string, keys []directoryKey, noise []string) (User, string, bool) {
	// The same handle stripped of the decorations that carry no identity.
	forms := []string{nameKey(handle)}

	trimmed := strings.ToLower(handle)
	for _, suffix := range noise {
		if strings.HasSuffix(trimmed, suffix) {
			forms = append(forms, nameKey(strings.TrimSuffix(trimmed, suffix)))
			break
		}
	}
	// A trailing counter — jsmith2, MarcusT4471 — is noise, not identity.
	if stripped := strings.TrimRight(forms[0], "0123456789"); stripped != forms[0] && stripped != "" {
		forms = append(forms, stripped)
	}

	for _, form := range forms {
		if u, basis, ok := matchAgainst(form, keys); ok {
			return u, basis, true
		}
	}
	return User{}, "", false
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
func SuggestAliases(unresolved []ClassifiedSeat, directory []User, domain string) []AliasSuggestion {
	return AttributeUnresolved(unresolved, directory, domain).Matched
}
