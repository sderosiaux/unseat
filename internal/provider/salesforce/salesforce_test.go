package salesforce

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token", "https://example.my.salesforce.com")
	assert.Equal(t, "salesforce", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "https://example.my.salesforce.com")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/services/data/v59.0/query")

		json.NewEncoder(w).Encode(queryResponse{
			Done:      true,
			TotalSize: 2,
			Records: []sfUser{
				{ID: "001A", Name: "Alice Smith", Email: "alice@co.com", IsActive: true, LastLoginDate: "2025-02-15T08:30:00Z", Profile: &struct {
					Name string `json:"Name"`
				}{Name: "System Administrator"}},
				{ID: "001B", Name: "Bob Jones", Email: "bob@co.com", IsActive: false, Profile: &struct {
					Name string `json:"Name"`
				}{Name: "Standard User"}},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "System Administrator", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "001A", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 2, 15, 8, 30, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Nil(t, users[1].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(queryResponse{
				Done:      false,
				TotalSize: 2,
				Records: []sfUser{
					{ID: "001A", Name: "Alice", Email: "alice@co.com", IsActive: true},
				},
				NextRecordsURL: "/services/data/v59.0/query/01gxx-2000",
			})
		} else {
			assert.Equal(t, "/services/data/v59.0/query/01gxx-2000", r.URL.Path)
			json.NewEncoder(w).Encode(queryResponse{
				Done:      true,
				TotalSize: 2,
				Records: []sfUser{
					{ID: "001B", Name: "Bob", Email: "bob@co.com", IsActive: true},
				},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "bob@co.com", users[1].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(queryResponse{
				Done: true,
				Records: []sfUser{
					{ID: "001A", Email: "alice@co.com", IsActive: true},
				},
			})
		} else {
			assert.Equal(t, http.MethodPatch, r.Method)
			assert.Equal(t, "/services/data/v59.0/sobjects/User/001A", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

			body, _ := io.ReadAll(r.Body)
			var payload map[string]bool
			json.Unmarshal(body, &payload)
			assert.Equal(t, false, payload["IsActive"])

			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(queryResponse{Done: true, Records: []sfUser{}})
	}))
	defer server.Close()

	p := New("test-token", server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`[{"message":"Session expired","errorCode":"INVALID_SESSION_ID"}]`))
	}))
	defer server.Close()

	p := New("bad-token", server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "https://example.my.salesforce.com")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "https://example.my.salesforce.com")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
