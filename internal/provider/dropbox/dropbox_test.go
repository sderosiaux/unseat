package dropbox

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
	p := New("test-token")
	assert.Equal(t, "dropbox", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/2/team/members/list_v2", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var req dropboxListRequest
		json.Unmarshal(body, &req)
		assert.Equal(t, 100, req.Limit)

		json.NewEncoder(w).Encode(dropboxListResponse{
			Members: []dropboxMember{
				{Profile: dropboxProfile{
					TeamMemberID: "tm1",
					Email:        "alice@co.com",
					Name:         dropboxName{DisplayName: "Alice Smith"},
					Status:       dropboxTag{Tag: "active"},
					Role:         dropboxTag{Tag: "team_admin"},
				}},
				{Profile: dropboxProfile{
					TeamMemberID: "tm2",
					Email:        "bob@co.com",
					Name:         dropboxName{DisplayName: "Bob Jones"},
					Status:       dropboxTag{Tag: "active"},
					Role:         dropboxTag{Tag: "member_only"},
				}},
			},
			HasMore: false,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "team_admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "tm1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "member_only", users[1].Role)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "/2/team/members/list_v2", r.URL.Path)
			json.NewEncoder(w).Encode(dropboxListResponse{
				Members: []dropboxMember{
					{Profile: dropboxProfile{TeamMemberID: "tm1", Email: "u1@co.com", Name: dropboxName{DisplayName: "User 1"}, Status: dropboxTag{Tag: "active"}}},
				},
				Cursor:  "cursor-xyz",
				HasMore: true,
			})
		} else {
			assert.Equal(t, "/2/team/members/list/continue_v2", r.URL.Path)
			body, _ := io.ReadAll(r.Body)
			var req dropboxContinueRequest
			json.Unmarshal(body, &req)
			assert.Equal(t, "cursor-xyz", req.Cursor)
			json.NewEncoder(w).Encode(dropboxListResponse{
				Members: []dropboxMember{
					{Profile: dropboxProfile{TeamMemberID: "tm2", Email: "u2@co.com", Name: dropboxName{DisplayName: "User 2"}, Status: dropboxTag{Tag: "active"}}},
				},
				HasMore: false,
			})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u2@co.com", users[1].Email)
}

func TestRemoveUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/2/team/members/remove", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		body, _ := io.ReadAll(r.Body)
		var req dropboxRemoveRequest
		json.Unmarshal(body, &req)
		assert.Equal(t, "email", req.User.Tag)
		assert.Equal(t, "alice@co.com", req.User.Email)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error_summary":"member_not_found/..","error":{".tag":"member_not_found"}}`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error_summary":"invalid_access_token/..","error":{".tag":"invalid_access_token"}}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
