package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-key", "mycompany")
	assert.Equal(t, "freshdesk", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-key", "mycompany")
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
		assert.Contains(t, r.URL.Path, "/api/v2/agents")

		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "test-key", user)
		assert.Equal(t, "X", pass)

		json.NewEncoder(w).Encode([]freshdeskAgent{
			{ID: 100, Contact: freshdeskContact{Email: "alice@co.com", Name: "Alice Smith"}, Occasional: false, Available: true},
			{ID: 200, Contact: freshdeskContact{Email: "bob@co.com", Name: "Bob Jones"}, Occasional: true, Available: false},
		})
	}))
	defer server.Close()

	p := New("test-key", "mycompany").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "agent", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "occasional", users[1].Role)
	assert.Equal(t, "unavailable", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			agents := make([]freshdeskAgent, 100)
			for i := range agents {
				agents[i] = freshdeskAgent{
					ID:      int64(i + 1),
					Contact: freshdeskContact{Email: fmt.Sprintf("user%d@co.com", i+1), Name: fmt.Sprintf("User %d", i+1)},
					Available: true,
				}
			}
			json.NewEncoder(w).Encode(agents)
		} else {
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode([]freshdeskAgent{
				{ID: 101, Contact: freshdeskContact{Email: "last@co.com", Name: "Last User"}, Available: true},
			})
		}
	}))
	defer server.Close()

	p := New("test-key", "mycompany").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 101)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode([]freshdeskAgent{
				{ID: 42, Contact: freshdeskContact{Email: "alice@co.com", Name: "Alice"}, Available: true},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v2/agents/42", r.URL.Path)
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "test-key", user)
			assert.Equal(t, "X", pass)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-key", "mycompany").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]freshdeskAgent{})
	}))
	defer server.Close()

	p := New("test-key", "mycompany").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":"invalid_credentials","message":"You have to be logged in to perform this action."}`))
	}))
	defer server.Close()

	p := New("bad-key", "mycompany").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-key", "mycompany")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-key", "mycompany")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
