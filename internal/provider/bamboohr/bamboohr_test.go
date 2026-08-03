package bamboohr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("api-key", "acme")
	assert.Equal(t, "bamboohr", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("api-key", "acme")
	caps := p.Capabilities()
	assert.False(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "api-key", user)
		assert.Equal(t, "x", pass)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/gateway.php/acme/v1/employees/directory", r.URL.Path)

		require.NoError(t, json.NewEncoder(w).Encode(directoryResponse{
			Employees: []directoryEmployee{
				{ID: "101", DisplayName: "Alice Smith", WorkEmail: "alice@co.com", JobTitle: "Engineer"},
				{ID: "102", DisplayName: "Bob Jones", WorkEmail: "bob@co.com", JobTitle: "Designer", Status: "Inactive"},
			},
		}))
	}))
	defer server.Close()

	p := New("api-key", "acme").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "101", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Equal(t, "102", users[1].ProviderID)
}

func TestNormalizeEmployeeStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		status string
	}{
		{name: "blank defaults active", input: "", status: "active"},
		{name: "active", input: "Active", status: "active"},
		{name: "current", input: "current", status: "active"},
		{name: "employed", input: " employed ", status: "active"},
		{name: "inactive", input: "Inactive", status: "suspended"},
		{name: "terminated", input: "terminated", status: "suspended"},
		{name: "unknown is not destructive", input: "leave_of_absence", status: "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.status, normalizeEmployeeStatus(tt.input))
		})
	}
}

func TestRemoveUserNotSupported(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	}))
	defer server.Close()

	p := New("api-key", "acme").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only HR identity source")
	assert.Equal(t, 0, callCount)
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, err := w.Write([]byte(`{"message":"Unauthorized"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	p := New("bad-key", "acme").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("api-key", "acme")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("api-key", "acme")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
