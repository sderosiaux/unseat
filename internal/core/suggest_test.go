package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seat(provider, id, name string) ClassifiedSeat {
	return ClassifiedSeat{
		Provider: provider,
		RawEmail: id,
		Class:    SeatUnresolved,
		User:     User{Email: id, DisplayName: name},
	}
}

func TestAttributeUnresolvedMatchesByName(t *testing.T) {
	dir := []User{
		{Email: "Julien@Co.com", DisplayName: "Julien Ferrand"},
		{Email: "amelie@co.com", DisplayName: "Amélie Brossard"},
	}

	rep := AttributeUnresolved([]ClassifiedSeat{
		seat("github", "jferrand", "Julien Ferrand"),
		// Accents on one side only must not defeat the match.
		seat("github", "AmelieBrossard", "Amélie Brossard"),
	}, dir, "co.com")

	require.Len(t, rep.Matched, 2)
	assert.Empty(t, rep.NamedButUnknown)
	assert.Empty(t, rep.Anonymous)
	assert.Equal(t, "amelie@co.com", rep.Matched[0].Email)
	assert.Equal(t, "julien@co.com", rep.Matched[1].Email, "emails are normalised")
}

// A handle that happens to look like the name without separators is still a
// real name. Normalising both sides before comparing them discarded exactly
// the seats that were matchable.
func TestAttributeUnresolvedKeepsNamesResemblingTheirHandle(t *testing.T) {
	dir := []User{{Email: "cschubert@co.com", DisplayName: "Christoph Schubert"}}

	rep := AttributeUnresolved([]ClassifiedSeat{
		seat("github", "christophschubert", "Christoph Schubert"),
	}, dir, "co.com")

	require.Len(t, rep.Matched, 1)
	assert.Empty(t, rep.Anonymous)
}

// When the connector had no name to report it echoes the handle verbatim.
// There is nothing to infer from that.
func TestAttributeUnresolvedAnonymousWhenNameIsTheHandle(t *testing.T) {
	rep := AttributeUnresolved([]ClassifiedSeat{
		seat("github", "nkoval", "nkoval"),
		seat("github", "LucasDbr", "lucasdbr"), // case-insensitive
		seat("github", "priyaven", ""),
	}, []User{{Email: "a@co.com", DisplayName: "Someone Else"}}, "co.com")

	assert.Len(t, rep.Anonymous, 3)
	assert.Empty(t, rep.Matched)
	assert.Empty(t, rep.NamedButUnknown)
}

// A real name matching nobody is the interesting case: probably a departure
// nobody finished. It is reported, never folded into a verdict.
func TestAttributeUnresolvedNamedButUnknown(t *testing.T) {
	rep := AttributeUnresolved([]ClassifiedSeat{
		seat("github", "silly-mid-on", "Ben Bramble"),
	}, []User{{Email: "a@co.com", DisplayName: "Someone Else"}}, "co.com")

	require.Len(t, rep.NamedButUnknown, 1)
	assert.Equal(t, "Ben Bramble", rep.NamedButUnknown[0].User.DisplayName)
	assert.Empty(t, rep.Matched)
}

// Two people sharing a name yield no proposal at all. Naming the wrong person
// in a report that drives deprovisioning is worse than leaving a gap visible.
func TestAttributeUnresolvedRefusesAmbiguousNames(t *testing.T) {
	dir := []User{
		{Email: "jsmith@co.com", DisplayName: "John Smith"},
		{Email: "john.smith@co.com", DisplayName: "John Smith"},
	}

	rep := AttributeUnresolved([]ClassifiedSeat{seat("github", "jsmith99", "John Smith")}, dir, "co.com")

	assert.Empty(t, rep.Matched)
	require.Len(t, rep.NamedButUnknown, 1, "ambiguity is reported, not silently dropped")
}

func TestNameKey(t *testing.T) {
	assert.Equal(t, "ameliebrossard", nameKey("Amélie Brossard"))
	assert.Equal(t, "jeanlouisboudart", nameKey("Jean-Louis Boudart"))
	assert.Equal(t, "niamhodwyer", nameKey("Niamh O'Dwyer"))
	assert.Equal(t, "", nameKey("   "))
}

// Handles are abbreviations of names, not names. Requiring equality attributed
// none of these, and each pattern here came from a real directory.
func TestAttributeUnresolvedReadsAbbreviatedHandles(t *testing.T) {
	dir := []User{
		{Email: "pvenkatesan@co.com", DisplayName: "Priya Venkatesan"},
		{Email: "marcus@co.com", DisplayName: "Marcus Tremblay"},
		{Email: "will.benton@co.com", DisplayName: "William Benton"},
		{Email: "raj.iyer@co.com", DisplayName: "Rajesh Iyer"},
		{Email: "bbramble@co.com", DisplayName: "Benjamin Bramble"},
		{Email: "ada.okafor@co.com", DisplayName: "Ada Okafor"},
	}

	cases := map[string]struct{ handle, name, want string }{
		"truncated name":      {"priyaven", "", "pvenkatesan@co.com"},
		"name plus counter":   {"MarcusT4471", "", "marcus@co.com"},
		"company suffix":      {"willbenton-acme", "", "will.benton@co.com"},
		"role suffix":         {"adaokafor-dev", "", "ada.okafor@co.com"},
		"short form in name":  {"raj-acme", "Raj Iyer", "raj.iyer@co.com"},
		"short form of given": {"silly-mid-on", "Ben Bramble", "bbramble@co.com"},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			s := seat("github", tc.handle, tc.name)
			if tc.name == "" {
				s.User.DisplayName = tc.handle
			}
			rep := AttributeUnresolved([]ClassifiedSeat{s}, dir, "acme.com")
			require.Len(t, rep.Matched, 1, "expected %s", tc.want)
			assert.Equal(t, tc.want, rep.Matched[0].Email)
		})
	}
}

// The company suffix is derived from the configured domain, not hardcoded.
func TestCompanyLabelDerivedFromDomain(t *testing.T) {
	assert.Equal(t, "acme", companyLabel("acme.com"))
	assert.Equal(t, "acme", companyLabel("ACME.co.uk"))
	assert.Empty(t, companyLabel(""))

	dir := []User{{Email: "will.benton@co.com", DisplayName: "William Benton"}}
	s := seat("github", "willbenton-acme", "willbenton-acme")

	assert.Len(t, AttributeUnresolved([]ClassifiedSeat{s}, dir, "acme.com").Matched, 1)
	// A different company's suffix is not stripped, so nothing is claimed.
	assert.Empty(t, AttributeUnresolved([]ClassifiedSeat{s}, dir, "other.com").Matched)
}

// Tolerance must not become guessing: two plausible owners means neither is
// named.
func TestAttributeUnresolvedStillRefusesAmbiguity(t *testing.T) {
	dir := []User{
		{Email: "a@co.com", DisplayName: "Chris Anderson"},
		{Email: "b@co.com", DisplayName: "Chris Andrews"},
	}
	s := seat("github", "chrisand", "chrisand")

	rep := AttributeUnresolved([]ClassifiedSeat{s}, dir, "co.com")
	assert.Empty(t, rep.Matched, "a prefix shared by two people names nobody")
}
