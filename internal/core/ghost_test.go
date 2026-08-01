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

	ghosts := FindGhostIdentities(seats, directory)

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

	ghosts := FindGhostIdentities(seats, directory)

	assert.Empty(t, ghosts)
}

func TestFindGhostIdentities_TrueExternalMatchingNobodyIsNotAGhost(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "contractor.jane@personal.com", "Jane Freelance"),
	}
	directory := []User{
		{Email: "omar.haddad@corp.io", DisplayName: "Omar Haddad", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory)

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

	ghosts := FindGhostIdentities(seats, directory)

	assert.Empty(t, ghosts)
}

func TestFindGhostIdentities_CaseAndAccentInsensitive(t *testing.T) {
	seats := []ClassifiedSeat{
		externalSeat("github", "camille@duflot.com", "CAMILLE DUFLOT"),
	}
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory)

	require.Len(t, ghosts, 1)
	assert.Equal(t, "camille@corp.io", ghosts[0].Email)
}

func TestFindGhostIdentities_MatchesOnLocalPartWhenNoUsableName(t *testing.T) {
	// No display name at all — the provider fell back to the raw address —
	// so the match has to come from the local part of the personal address.
	seats := []ClassifiedSeat{
		externalSeat("github", "camiller@personal.com", ""),
	}
	directory := []User{
		{Email: "camille@corp.io", DisplayName: "Camille Duflot", Status: StatusActive},
	}

	ghosts := FindGhostIdentities(seats, directory)

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

	ghosts := FindGhostIdentities(seats, directory)

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

	assert.Empty(t, FindGhostIdentities(seats, directory),
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

	assert.Empty(t, FindGhostIdentities(seats, directory))
}
