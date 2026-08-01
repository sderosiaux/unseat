package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
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

		// Raw JSON in the shape Freshdesk actually returns, NOT an encoded
		// freshdeskAgent. Marshalling our own struct round-trips perfectly and
		// would hide a misplaced field — which is exactly how `active` and
		// `last_login_at` sat on the wrong struct without a test noticing.
		fmt.Fprint(w, `[
		  {"id":100,"occasional":false,"available":true,
		   "contact":{"name":"Alice Smith","email":"alice@co.com","active":true,"last_login_at":"2025-02-28T15:00:00Z"}},
		  {"id":200,"occasional":true,"available":false,
		   "contact":{"name":"Bob Jones","email":"bob@co.com","active":true}},
		  {"id":300,"occasional":false,"available":false,
		   "contact":{"name":"Carol Diaz","email":"carol@co.com","active":false}}
		]`)
	}))
	defer server.Close()

	p := New("test-key", "mycompany").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "agent", users[0].Role)
	assert.Equal(t, core.StatusActive, users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 2, 28, 15, 0, 0, 0, time.UTC), *users[0].LastActivityAt)

	// Availability is a routing toggle, not account state. Reporting it as
	// suspended would make reconciliation treat a working agent as already
	// deactivated and stop reclaiming the seat when they actually leave.
	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "occasional", users[1].Role)
	assert.Equal(t, core.StatusActive, users[1].Status)
	assert.Equal(t, "false", users[1].Metadata["available"])
	assert.Nil(t, users[1].LastActivityAt)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, core.StatusSuspended, users[2].Status)
}

// If `active` is missing from the payload, the agent must be treated as active.
// Defaulting a missing field to "deactivated" would report an entire helpdesk
// as suspended, which reconciliation reads as "already deprovisioned".
func TestListUsersMissingActiveDefaultsToActive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":1,"available":true,"contact":{"name":"Dana","email":"dana@co.com"}}]`)
	}))
	defer server.Close()

	users, err := New("test-key", "mycompany").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, core.StatusActive, users[0].Status)
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
					ID:        int64(i + 1),
					Contact:   freshdeskContact{Email: fmt.Sprintf("user%d@co.com", i+1), Name: fmt.Sprintf("User %d", i+1)},
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
