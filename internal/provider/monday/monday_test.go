package monday

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
	p := New("test-key")
	assert.Equal(t, "monday", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-key")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body gqlRequest
		json.NewDecoder(r.Body).Decode(&body)
		assert.Contains(t, body.Query, "users")

		json.NewEncoder(w).Encode(gqlResponse{
			Data: mustMarshal(listUsersData{
				Users: []mondayUser{
					{ID: toJSONNumber("111"), Name: "Alice Smith", Email: "alice@co.com", IsAdmin: true, Enabled: true},
					{ID: toJSONNumber("222"), Name: "Bob Jones", Email: "bob@co.com", IsGuest: true, Enabled: true},
					{ID: toJSONNumber("333"), Name: "Charlie", Email: "charlie@co.com", Enabled: false},
				},
			}),
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "111", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "guest", users[1].Role)

	assert.Equal(t, "charlie@co.com", users[2].Email)
	assert.Equal(t, "suspended", users[2].Status)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body gqlRequest
		json.NewDecoder(r.Body).Decode(&body)

		if callCount == 1 {
			// ListUsers query
			json.NewEncoder(w).Encode(gqlResponse{
				Data: mustMarshal(listUsersData{
					Users: []mondayUser{
						{ID: toJSONNumber("111"), Name: "Alice", Email: "alice@co.com", Enabled: true},
					},
				}),
			})
		} else {
			// Deactivate mutation
			assert.Contains(t, body.Query, "deactivate_users")
			assert.Contains(t, body.Query, "111")
			json.NewEncoder(w).Encode(gqlResponse{
				Data: mustMarshal(map[string]any{
					"deactivate_users": map[string]any{
						"deactivated_users": []map[string]any{{"id": 111}},
					},
				}),
			})
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gqlResponse{
			Data: mustMarshal(listUsersData{Users: []mondayUser{}}),
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error_message":"Not Authenticated"}`))
	}))
	defer server.Close()

	p := New("bad-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func toJSONNumber(s string) json.Number {
	return json.Number(s)
}
