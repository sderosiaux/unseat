package lastpass

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-cid", "test-hash")
	assert.Equal(t, "lastpass", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-cid", "test-hash")
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
		assert.Equal(t, "/enterpriseapi.php", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req apiRequest
		json.Unmarshal(body, &req)
		assert.Equal(t, "test-cid", req.CID)
		assert.Equal(t, "test-hash", req.ProvHash)
		assert.Equal(t, "getuserdata", req.Cmd)

		json.NewEncoder(w).Encode(getUserDataResponse{
			Users: map[string]userData{
				"101": {UserName: "alice@co.com", FullName: "Alice Smith", Admin: true, Disabled: false},
				"102": {UserName: "bob@co.com", FullName: "Bob Jones", Admin: false, Disabled: true},
				"103": {UserName: "carol@co.com", FullName: "Carol White", Admin: false, Invited: true},
			},
			Total: 3,
		})
	}))
	defer server.Close()

	p := New("test-cid", "test-hash").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	// Map by email for deterministic assertions (map iteration order is random).
	byEmail := map[string]struct{ Role, Status string }{}
	for _, u := range users {
		byEmail[u.Email] = struct{ Role, Status string }{u.Role, u.Status}
	}

	assert.Equal(t, "admin", byEmail["alice@co.com"].Role)
	assert.Equal(t, "active", byEmail["alice@co.com"].Status)
	assert.Equal(t, "member", byEmail["bob@co.com"].Role)
	assert.Equal(t, "suspended", byEmail["bob@co.com"].Status)
	assert.Equal(t, "invited", byEmail["carol@co.com"].Status)
}

func TestRemoveUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/enterpriseapi.php", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var req apiRequest
		json.Unmarshal(body, &req)
		assert.Equal(t, "deluser", req.Cmd)

		dataMap, ok := req.Data.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "alice@co.com", dataMap["username"])

		json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	}))
	defer server.Close()

	p := New("test-cid", "test-hash").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "FAIL"})
	}))
	defer server.Close()

	p := New("test-cid", "test-hash").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove user failed")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"invalid provisioning hash"}`))
	}))
	defer server.Close()

	p := New("bad-cid", "bad-hash").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-cid", "test-hash")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-cid", "test-hash")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
