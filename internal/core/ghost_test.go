package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func externalSeat(provider, rawEmail, displayName string) ClassifiedSeat {
	return ClassifiedSeat{
		Provider: provider,
		Email:    rawEmail,
		RawEmail: rawEmail,
		Class:    SeatExternal,
		User: User{
			Email:       rawEmail,
			DisplayName: displayName,
		},
	}
}

func TestFindGhostIdentities_ActiveEmployeeOnPersonalAddress(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "omarhaddad2017@gmail.com", "Omar Haddad"),
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	require.Len(t, ghosts, 1)
	assert.Equal(t, "github", ghosts[0].Provider)
	assert.Equal(t, "omarhaddad2017@gmail.com", ghosts[0].Identifier)
	assert.Equal(t, "omar.haddad@corp.io", ghosts[0].Email)
	assert.NotEmpty(t, ghosts[0].Basis)
}

func TestFindGhostIdentities_SameIdentitySuspendedIsNotAGhost(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "omarhaddad2017@gmail.com", "Omar Haddad"),
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: StatusSuspended},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	assert.Empty(t, ghosts)
}

func TestFindGhostIdentities_TrueExternalMatchingNobodyIsNotAGhost(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "contractor.jane@personal.com", "Jane Freelance"),
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	assert.Empty(t, ghosts)
}

func TestFindGhostIdentities_AmbiguityYieldsNothing(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "jsmithpersonal@gmail.com", "John Smith"),
	}
	// Two directory identities share the same normalised name — the shared
	// matching machinery refuses to pick either, and the ghost report must
	// honour the same refusal rather than guessing.
	directory := []User{
		{Email: "john.smith@corp.io", DisplayName: "John Smith", Status: StatusActive},
		{Email: "j.smith2@corp.io", DisplayName: "John Smith", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	assert.Empty(t, ghosts)
}

func TestFindGhostIdentities_CaseAndAccentInsensitive(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "camille@duflot.com", "CAMILLE DUFLOT"),
	}
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	require.Len(t, ghosts, 1)
	assert.Equal(t, "camille@corp.io", ghosts[0].Email)
}

func TestFindGhostIdentities_MatchesOnLocalPartWhenNoUsableName(t *testing.T) {
	// No display name at all — the provider fell back to the raw address —
	// so the match has to come from the local part of the personal address.
	seats := []ClassifiedSeat{
		externalSeat("github", "camilled@personal.com", ""),
	}
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	require.Len(t, ghosts, 1)
	assert.Equal(t, "camille@corp.io", ghosts[0].Email)
}

func TestFindGhostIdentities_IgnoresNonExternalSeats(t *testing.T) {
	seats := []ClassifiedSeat{
		{
			Provider: "github",
			RawEmail: "omar.haddad@corp.io",
			Class:    SeatManaged,
			User:     User{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad"},
		},
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	assert.Empty(t, ghosts)
}

// A stated name that matches nobody is evidence AGAINST a match, not an absence
// of evidence. Falling through to the weaker branches let the address local
// part claim a different colleague entirely.
func TestFindGhostIdentitiesStatedNameVetoesWeakerBranches(t *testing.T) {
	directory := []User{
		{Email: "john.smith@corp.io", DisplayName: "John Smith", Status: StatusActive},
	}
	// The provider says Jane. "jsmith" would otherwise match John.
	seats := []ClassifiedSeat{{
		Provider: "github",
		RawEmail: "jsmith@vendor.io",
		Class:    SeatExternal,
		User:     User{Email: "jsmith@vendor.io", DisplayName: "Jane Smith"},
	}}

	assert.Empty(t, FindGhostIdentities(seats, directory, "corp.io"),
		"the provider named someone the directory does not have — that contradicts, it does not merely fail to confirm")
}

// Without a stated name the local part may still speak, but only when exactly
// one identity can claim it.
func TestFindGhostIdentitiesAmbiguousLocalPartNamesNobody(t *testing.T) {
	directory := []User{
		{Email: "john.smith@corp.io", DisplayName: "John Smith", Status: StatusActive},
		{Email: "jane.smith@corp.io", DisplayName: "Jane Smith", Status: StatusActive},
	}
	seats := []ClassifiedSeat{{
		Provider: "github",
		RawEmail: "jsmith@vendor.io",
		Class:    SeatExternal,
		User:     User{Email: "jsmith@vendor.io"},
	}}

	assert.Empty(t, FindGhostIdentities(seats, directory, "corp.io"))
}

// A directory identity with no Status set (or one this codebase does not
// recognise) must not pass as active. Checking match.Status == StatusActive
// rather than match.Status != StatusSuspended is what stops that — the
// deny-list version would let this straight through.
func TestFindGhostIdentitiesRequiresExplicitActiveStatus(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "omarhaddad2017@gmail.com", "Omar Haddad"),
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: ""},
	}

	assert.Empty(t, FindGhostIdentities(seats, directory, "corp.io"))
}

// The provider-assigned login (GitHub's handle, distinct from the address
// signed up with) is the third and weakest matching branch. It has to carry a
// match on its own when neither the display name nor the address local part
// says anything usable.
func TestFindGhostIdentitiesMatchesOnProviderLogin(t *testing.T) {
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
	}
	seats := []ClassifiedSeat{{
		Provider: "github",
		RawEmail: "randomcontractor99@personal.com",
		Class:    SeatExternal,
		User: User{
			Email:    "randomcontractor99@personal.com",
			Metadata: map[string]string{"login": "camilled"},
		},
	}}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	require.Len(t, ghosts, 1)
	assert.Equal(t, "camille@corp.io", ghosts[0].Email)
	assert.Contains(t, ghosts[0].Basis, "handle")
}

// When the local part of the address already answers the match, the login
// branch must never run — even when the login spells a different, equally
// real directory identity. Reaching it anyway would let a provider-assigned
// handle silently override a stronger signal that already resolved.
func TestFindGhostIdentitiesLoginNotReachedWhenLocalPartAlreadyMatched(t *testing.T) {
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
		{Email: "unrelated@corp.io", DisplayName: "Unrelated Login", Status: StatusActive},
	}
	seats := []ClassifiedSeat{{
		Provider: "github",
		RawEmail: "camilled@personal.com",
		Class:    SeatExternal,
		User: User{
			Email:    "camilled@personal.com",
			Metadata: map[string]string{"login": "unrelatedlogin"},
		},
	}}

	ghosts := FindGhostIdentities(seats, directory, "corp.io")

	require.Len(t, ghosts, 1)
	assert.Equal(t, "camille@corp.io", ghosts[0].Email,
		"the login would have named a different, equally valid identity — it must not have been consulted")
	assert.Contains(t, ghosts[0].Basis, "local part")
}

// The company label stripped from handles in AttributeUnresolved must apply
// here too, or a seat like "someone-acme" is a ghost in one report and
// invisible in the other for no reason a reader can see.
func TestFindGhostIdentitiesStripsCompanyLabelFromHandle(t *testing.T) {
	directory := []User{
		{Email: "will.benton@corp.io", DisplayName: "William Benton", Status: StatusActive},
	}
	seats := []ClassifiedSeat{{
		Provider: "github",
		RawEmail: "willbenton-acme@personal.com",
		Class:    SeatExternal,
		User:     User{Email: "willbenton-acme@personal.com"},
	}}

	ghosts := FindGhostIdentities(seats, directory, "acme.com")
	require.Len(t, ghosts, 1)
	assert.Equal(t, "will.benton@corp.io", ghosts[0].Email)

	// Without the domain to derive the label from, the same handle carries
	// no usable signal — the company suffix is exactly what made it readable.
	assert.Empty(t, FindGhostIdentities(seats, directory, ""))
}
