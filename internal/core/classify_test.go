package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDirectory is the shared corporate directory used by the classification
// tests. Keys are lowercased, as ClassifySeats documents the caller must do.
func testDirectory() map[string]DirectoryUser {
	return map[string]DirectoryUser{
		"alice@co.com":     {Email: "alice@co.com"},
		"bob@co.com":       {Email: "bob@co.com"},
		"cto@co.com":       {Email: "cto@co.com"},
		"jmartinez@co.com": {Email: "jmartinez@co.com"},
		"river@co.com":     {Email: "river@co.com"},
		"gone@co.com":      {Email: "gone@co.com", Suspended: true},
	}
}

func baseInput() ClassifyInput {
	return ClassifyInput{
		ProviderName:  "figma",
		Directory:     testDirectory(),
		DesiredEmails: map[string]bool{"alice@co.com": true, "gone@co.com": true, "jmartinez@co.com": true},
		Domain:        "co.com",
	}
}

// classifyOne runs ClassifySeats on a single provider seat and returns it.
func classifyOne(t *testing.T, input ClassifyInput, u User) ClassifiedSeat {
	t.Helper()
	input.ActualUsers = []User{u}
	seats := ClassifySeats(input)
	require.Len(t, seats, 1)
	return seats[0]
}

func TestClassifySeats_Classes(t *testing.T) {
	tests := []struct {
		name          string
		user          User
		wantClass     SeatClass
		wantReasonSub string
	}{
		{
			name:          "managed: active directory user inside a mapped group",
			user:          User{Email: "alice@co.com"},
			wantClass:     SeatManaged,
			wantReasonSub: "member of a group mapped",
		},
		{
			name:          "unmapped: active directory user outside every mapped group",
			user:          User{Email: "bob@co.com"},
			wantClass:     SeatUnmapped,
			wantReasonSub: "active employee",
		},
		{
			name:          "orphan: absent from the directory",
			user:          User{Email: "ghost@co.com"},
			wantClass:     SeatOrphan,
			wantReasonSub: "no matching identity in the directory",
		},
		{
			name:          "orphan: present but suspended in the directory",
			user:          User{Email: "gone@co.com"},
			wantClass:     SeatOrphan,
			wantReasonSub: "directory identity is suspended",
		},
		{
			name:          "external: identity outside the corporate domain",
			user:          User{Email: "freelance@gmail.com"},
			wantClass:     SeatExternal,
			wantReasonSub: "outside the corporate domain",
		},
		{
			name:          "unresolved: provider username with no email and no alias",
			user:          User{Email: "jenkins-bot"},
			wantClass:     SeatUnresolved,
			wantReasonSub: "username with no email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seat := classifyOne(t, baseInput(), tt.user)
			assert.Equal(t, tt.wantClass, seat.Class)
			assert.Contains(t, seat.Reason, tt.wantReasonSub)
			assert.Equal(t, "figma", seat.Provider)
			assert.Equal(t, tt.user, seat.User)
		})
	}
}

// A suspended directory identity is an orphan even when the mapped groups still
// list it: the money is in the seat, not in the stale group membership.
func TestClassifySeats_SuspendedBeatsDesired(t *testing.T) {
	in := baseInput()
	require.True(t, in.DesiredEmails["gone@co.com"], "fixture must keep the suspended user desired")

	seat := classifyOne(t, in, User{Email: "gone@co.com"})
	assert.Equal(t, SeatOrphan, seat.Class)
	assert.Contains(t, seat.Reason, "suspended")
}

// Precedence is the part most likely to regress: each guard must win over the
// ones below it, otherwise unseat reclaims seats it has no business touching.
func TestClassifySeats_Precedence(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ClassifyInput)
		user      User
		wantClass SeatClass
		why       string
	}{
		{
			name:      "username with no @ is unresolved even with a domain configured",
			user:      User{Email: "jenkins-bot"},
			wantClass: SeatUnresolved,
			why:       "a bare username must never be judged against the domain",
		},
		{
			name:      "out-of-domain email is external even though absent from the directory",
			user:      User{Email: "contractor@agency.io"},
			wantClass: SeatExternal,
			why:       "an external identity must not be reported as a reclaimable orphan",
		},
		{
			name:      "nil directory yields unresolved for an in-domain user",
			mutate:    func(in *ClassifyInput) { in.Directory = nil },
			user:      User{Email: "alice@co.com"},
			wantClass: SeatUnresolved,
			why:       "an unreadable directory must not turn every employee into an orphan",
		},
		{
			name:      "nil directory still classifies out-of-domain as external",
			mutate:    func(in *ClassifyInput) { in.Directory = nil },
			user:      User{Email: "freelance@gmail.com"},
			wantClass: SeatExternal,
			why:       "the domain check runs before the directory availability check",
		},
		{
			name:      "nil directory still classifies a bare username as unresolved",
			mutate:    func(in *ClassifyInput) { in.Directory = nil },
			user:      User{Email: "jenkins-bot"},
			wantClass: SeatUnresolved,
			why:       "no directory and no email is still unresolved",
		},
		{
			name:      "empty (non-nil) directory yields orphan, unlike a nil directory",
			mutate:    func(in *ClassifyInput) { in.Directory = map[string]DirectoryUser{} },
			user:      User{Email: "alice@co.com"},
			wantClass: SeatOrphan,
			why:       "an empty directory is an answer; a nil directory is a failure",
		},
		{
			name:      "empty email is unresolved, not external",
			user:      User{Email: ""},
			wantClass: SeatUnresolved,
			why:       "an empty identity contains no @ and cannot be judged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput()
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			seat := classifyOne(t, in, tt.user)
			assert.Equal(t, tt.wantClass, seat.Class, tt.why)
			assert.False(t, seat.Reclaimable() && tt.wantClass != SeatOrphan,
				"only orphans may ever be reclaimable")
		})
	}
}

func TestClassifySeats_AliasResolution(t *testing.T) {
	in := baseInput()
	in.AliasIndex = BuildAliasIndex(
		map[string][]string{"river@co.com": {"river@personal.net"}},
		[]string{"alice@co.com", "jmartinez@co.com", "river@co.com"},
	)

	tests := []struct {
		name      string
		raw       string
		wantEmail string
		wantClass SeatClass
	}{
		{
			name:      "implicit alias: username resolves to the corporate email",
			raw:       "jmartinez",
			wantEmail: "jmartinez@co.com",
			wantClass: SeatManaged,
		},
		{
			name:      "explicit alias: personal email resolves before the domain check",
			raw:       "river@personal.net",
			wantEmail: "river@co.com",
			wantClass: SeatUnmapped, // in the directory, not in a mapped group
		},
		{
			name:      "mixed-case raw value still hits the lowercased alias index",
			raw:       "JMartinez",
			wantEmail: "jmartinez@co.com",
			wantClass: SeatManaged,
		},
		{
			name:      "unknown alias falls back to the lowercased raw value",
			raw:       "Ghost@Co.com",
			wantEmail: "ghost@co.com",
			wantClass: SeatOrphan,
		},
		{
			name:      "unknown username with no alias stays unresolved",
			raw:       "mystery-bot",
			wantEmail: "mystery-bot",
			wantClass: SeatUnresolved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seat := classifyOne(t, in, User{Email: tt.raw})
			assert.Equal(t, tt.wantEmail, seat.Email, "must classify against the resolved email")
			assert.Equal(t, tt.raw, seat.RawEmail, "RawEmail must retain what the provider reported")
			assert.Equal(t, tt.wantClass, seat.Class)
		})
	}
}

func TestClassifySeats_Protected(t *testing.T) {
	in := baseInput()
	in.Exceptions = map[string]bool{
		"cto@co.com":   true,
		"ghost@co.com": true,
		"bot-ci":       true,
	}

	tests := []struct {
		name            string
		raw             string
		wantClass       SeatClass
		wantProtected   bool
		wantReclaimable bool
	}{
		{
			name:            "protected orphan is never reclaimable",
			raw:             "ghost@co.com",
			wantClass:       SeatOrphan,
			wantProtected:   true,
			wantReclaimable: false,
		},
		{
			name:            "unprotected orphan is reclaimable",
			raw:             "phantom@co.com",
			wantClass:       SeatOrphan,
			wantProtected:   false,
			wantReclaimable: true,
		},
		{
			name:            "protected managed seat is not reclaimable",
			raw:             "cto@co.com",
			wantClass:       SeatUnmapped,
			wantProtected:   true,
			wantReclaimable: false,
		},
		{
			name:            "exceptions apply to unresolved usernames too",
			raw:             "bot-ci",
			wantClass:       SeatUnresolved,
			wantProtected:   true,
			wantReclaimable: false,
		},
		{
			name:            "exception matching is case-insensitive on the provider value",
			raw:             "GHOST@CO.COM",
			wantClass:       SeatOrphan,
			wantProtected:   true,
			wantReclaimable: false,
		},
		{
			name:            "external seat is never reclaimable even unprotected",
			raw:             "freelance@gmail.com",
			wantClass:       SeatExternal,
			wantProtected:   false,
			wantReclaimable: false,
		},
		{
			name:            "managed seat is never reclaimable",
			raw:             "alice@co.com",
			wantClass:       SeatManaged,
			wantProtected:   false,
			wantReclaimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seat := classifyOne(t, in, User{Email: tt.raw})
			assert.Equal(t, tt.wantClass, seat.Class)
			assert.Equal(t, tt.wantProtected, seat.Protected)
			assert.Equal(t, tt.wantReclaimable, seat.Reclaimable())
		})
	}
}

func TestClassifySeats_NilOptionalMaps(t *testing.T) {
	seat := classifyOne(t, ClassifyInput{
		ProviderName: "figma",
		Directory:    testDirectory(),
		Domain:       "co.com",
		// DesiredEmails, AliasIndex and Exceptions all nil.
	}, User{Email: "alice@co.com"})

	assert.Equal(t, SeatUnmapped, seat.Class, "nil DesiredEmails means nothing is mapped")
	assert.False(t, seat.Protected)
}

func TestClassifySeats_CaseInsensitivity(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		raw       string
		wantEmail string
		wantClass SeatClass
	}{
		{
			name:      "mixed-case provider email matches the lowercased directory key",
			domain:    "co.com",
			raw:       "ALICE@CO.COM",
			wantEmail: "alice@co.com",
			wantClass: SeatManaged,
		},
		{
			name:      "mixed-case domain in config still matches",
			domain:    "Co.Com",
			raw:       "Bob@Co.com",
			wantEmail: "bob@co.com",
			wantClass: SeatUnmapped,
		},
		{
			name:      "domain written with a leading @ is normalized",
			domain:    "@co.com",
			raw:       "alice@co.com",
			wantEmail: "alice@co.com",
			wantClass: SeatManaged,
		},
		{
			name:      "surrounding whitespace is trimmed off the provider value",
			domain:    "co.com",
			raw:       "  alice@co.com  ",
			wantEmail: "alice@co.com",
			wantClass: SeatManaged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseInput()
			in.Domain = tt.domain
			seat := classifyOne(t, in, User{Email: tt.raw})
			assert.Equal(t, tt.wantEmail, seat.Email)
			assert.Equal(t, tt.raw, seat.RawEmail)
			assert.Equal(t, tt.wantClass, seat.Class)
		})
	}
}

func TestClassifySeats_EmptyDomain(t *testing.T) {
	in := baseInput()
	in.Domain = ""
	in.ActualUsers = []User{
		{Email: "alice@co.com"},
		{Email: "bob@co.com"},
		{Email: "freelance@gmail.com"},
		{Email: "gone@co.com"},
		{Email: "jenkins-bot"},
	}

	seats := ClassifySeats(in)
	require.Len(t, seats, 5)
	for _, seat := range seats {
		assert.NotEqual(t, SeatExternal, seat.Class,
			"no seat may be external when no corporate domain is configured (%s)", seat.Email)
	}
	assert.Equal(t, SeatManaged, seats[0].Class)
	assert.Equal(t, SeatUnmapped, seats[1].Class)
	assert.Equal(t, SeatOrphan, seats[2].Class, "with no domain, an unknown gmail is just an orphan")
	assert.Equal(t, SeatOrphan, seats[3].Class)
	assert.Equal(t, SeatUnresolved, seats[4].Class)
}

func TestClassifySeats_WhitespaceOnlyDomain(t *testing.T) {
	in := baseInput()
	in.Domain = "   "
	seat := classifyOne(t, in, User{Email: "freelance@gmail.com"})
	assert.Equal(t, SeatOrphan, seat.Class, "a blank domain must behave like no domain at all")
}

func TestClassifySeats_NoUsers(t *testing.T) {
	in := baseInput()
	in.ActualUsers = nil

	seats := ClassifySeats(in)
	assert.NotNil(t, seats, "must return an empty slice, not nil, so it marshals as []")
	assert.Empty(t, seats)
}

func TestClassifySeats_PreservesOrderAndUser(t *testing.T) {
	in := baseInput()
	in.ActualUsers = []User{
		{Email: "bob@co.com", DisplayName: "Bob", Role: "editor", Status: "active", ProviderID: "u2"},
		{Email: "alice@co.com", DisplayName: "Alice", Role: "admin", Status: "active", ProviderID: "u1"},
	}

	seats := ClassifySeats(in)
	require.Len(t, seats, 2)
	assert.Equal(t, "bob@co.com", seats[0].Email)
	assert.Equal(t, "alice@co.com", seats[1].Email)
	assert.Equal(t, "u2", seats[0].User.ProviderID, "the full provider User must be carried through")
	assert.Equal(t, "admin", seats[1].User.Role)
}

func TestSummarizeSeats(t *testing.T) {
	in := baseInput()
	in.Exceptions = map[string]bool{
		"alice@co.com":        true, // managed + protected
		"ghost@co.com":        true, // orphan + protected
		"freelance@gmail.com": true, // external + protected
	}
	in.ActualUsers = []User{
		{Email: "alice@co.com"},        // managed, protected
		{Email: "jmartinez@co.com"},    // managed
		{Email: "bob@co.com"},          // unmapped
		{Email: "cto@co.com"},          // unmapped
		{Email: "ghost@co.com"},        // orphan (absent), protected
		{Email: "phantom@co.com"},      // orphan (absent)
		{Email: "gone@co.com"},         // orphan (suspended)
		{Email: "freelance@gmail.com"}, // external, protected
		{Email: "jenkins-bot"},         // unresolved
	}

	summary := SummarizeSeats("figma", ClassifySeats(in))

	assert.Equal(t, "figma", summary.Provider)
	assert.Equal(t, 2, summary.Managed)
	assert.Equal(t, 2, summary.Unmapped)
	assert.Equal(t, 3, summary.Orphan)
	assert.Equal(t, 1, summary.External)
	assert.Equal(t, 1, summary.Unresolved)
	assert.Equal(t, 3, summary.Protected, "protected is counted across classes, not as a class")
	assert.Equal(t, 9, summary.Total)
	assert.Equal(t,
		summary.Total,
		summary.Managed+summary.Unmapped+summary.Orphan+summary.External+summary.Unresolved,
		"every seat must land in exactly one class")
}

func TestSummarizeSeats_Empty(t *testing.T) {
	summary := SummarizeSeats("linear", nil)
	assert.Equal(t, ClassSummary{Provider: "linear"}, summary)
}

func TestSummarizeSeats_ProtectedIsIndependentOfClass(t *testing.T) {
	seats := []ClassifiedSeat{
		{Class: SeatManaged, Protected: true},
		{Class: SeatOrphan, Protected: true},
		{Class: SeatUnresolved, Protected: true},
	}

	summary := SummarizeSeats("miro", seats)
	assert.Equal(t, 3, summary.Protected)
	assert.Equal(t, 1, summary.Managed)
	assert.Equal(t, 1, summary.Orphan)
	assert.Equal(t, 1, summary.Unresolved)
	assert.Equal(t, 3, summary.Total)
}
