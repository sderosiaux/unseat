package framer

import (
	"context"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
)

func TestName(t *testing.T) {
	p := New()
	if got := p.Name(); got != "framer" {
		t.Fatalf("Name() = %q, want %q", got, "framer")
	}
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	want := core.Capabilities{}
	if caps != want {
		t.Fatalf("Capabilities() = %+v, want all false %+v", caps, want)
	}
}

func TestListUsers(t *testing.T) {
	p := New()
	users, err := p.ListUsers(context.Background())
	if users != nil {
		t.Fatalf("ListUsers() returned non-nil users: %v", users)
	}
	if err == nil {
		t.Fatal("ListUsers() expected error, got nil")
	}
	if err.Error() != "framer: no public API for user management" {
		t.Fatalf("ListUsers() error = %q, want framer stub message", err.Error())
	}
}

func TestAddUser(t *testing.T) {
	p := New()
	err := p.AddUser(context.Background(), "a@b.com", "member")
	if err == nil {
		t.Fatal("AddUser() expected error, got nil")
	}
	if err.Error() != "framer: no public API for user management" {
		t.Fatalf("AddUser() error = %q, want framer stub message", err.Error())
	}
}

func TestRemoveUser(t *testing.T) {
	p := New()
	err := p.RemoveUser(context.Background(), "a@b.com")
	if err == nil {
		t.Fatal("RemoveUser() expected error, got nil")
	}
	if err.Error() != "framer: no public API for user management" {
		t.Fatalf("RemoveUser() error = %q, want framer stub message", err.Error())
	}
}

func TestSetRole(t *testing.T) {
	p := New()
	err := p.SetRole(context.Background(), "a@b.com", "admin")
	if err == nil {
		t.Fatal("SetRole() expected error, got nil")
	}
	if err.Error() != "framer: no public API for user management" {
		t.Fatalf("SetRole() error = %q, want framer stub message", err.Error())
	}
}
