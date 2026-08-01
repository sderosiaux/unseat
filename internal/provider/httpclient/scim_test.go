package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scimTestUser struct {
	ID       string `json:"id"`
	UserName string `json:"userName"`
}

func scimServer(t *testing.T, handler func(startIndex, count int) SCIMListResponse[scimTestUser]) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		require.NoError(t, json.NewEncoder(w).Encode(handler(startIndex, count)))
	}))
}

func TestListSCIMWalksAllPages(t *testing.T) {
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		total := 250
		var res []scimTestUser
		for i := startIndex; i < startIndex+count && i <= total; i++ {
			res = append(res, scimTestUser{ID: strconv.Itoa(i), UserName: "u" + strconv.Itoa(i)})
		}
		return SCIMListResponse[scimTestUser]{Resources: res, TotalResults: total, ItemsPerPage: len(res), StartIndex: startIndex}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL + "/scim/v2/Users",
	})
	require.NoError(t, err)
	assert.Len(t, users, 250, "every page must be collected, not just the first")
	assert.Equal(t, "u1", users[0].UserName)
	assert.Equal(t, "u250", users[249].UserName)
}

// The defect this helper exists to kill: a server that reports more results
// than it returns used to spin the caller forever, with no timeout to stop it.
func TestListSCIMTerminatesOnEmptyPageDespiteHighTotal(t *testing.T) {
	var calls atomic.Int32
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		calls.Add(1)
		if startIndex == 1 {
			return SCIMListResponse[scimTestUser]{
				Resources:    []scimTestUser{{ID: "1", UserName: "u1"}},
				TotalResults: 9999,
			}
		}
		// Claims thousands more, delivers none.
		return SCIMListResponse[scimTestUser]{Resources: nil, TotalResults: 9999}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _ := testClient(t)
	done := make(chan struct{})
	var users []scimTestUser
	var err error
	go func() {
		users, err = ListSCIM[scimTestUser](ctx, c, SCIMPageOptions{Provider: "test", URL: srv.URL})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListSCIM did not terminate — the infinite pagination loop is back")
	}

	// Terminating is necessary but not sufficient: returning the partial list
	// with a nil error would let a truncated inventory overwrite the cache and
	// make every missing user look absent from the SaaS.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pagination stalled")
	assert.Nil(t, users)
	assert.EqualValues(t, 2, calls.Load(), "the walk stops at the first empty page")
}

// Regression: a server that omits totalResults AND ignores startIndex used to
// be handled by the old hand-rolled guard, which broke on the first page. The
// shared helper must not walk it to the page cap and fail with zero users.
func TestListSCIMTerminatesWhenTotalResultsOmitted(t *testing.T) {
	var calls atomic.Int32
	srv := scimServer(t, func(_, _ int) SCIMListResponse[scimTestUser] {
		calls.Add(1)
		// Same short page every time, no total, startIndex ignored.
		return SCIMListResponse[scimTestUser]{
			Resources: []scimTestUser{{ID: "1"}, {ID: "2"}},
		}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL, PageSize: 100,
	})
	require.NoError(t, err, "a short page ends the walk; it is not a stall")
	assert.Len(t, users, 2)
	assert.EqualValues(t, 1, calls.Load(), "one request, not a thousand")
}

// Servers routinely cap page size below what the client requests. Every page
// is then "short", and treating that as the end would truncate the inventory —
// which downstream reads as "these users left the SaaS".
func TestListSCIMFollowsServerCappedPageSize(t *testing.T) {
	const serverCap = 50
	srv := scimServer(t, func(startIndex, _ int) SCIMListResponse[scimTestUser] {
		total := 120
		var res []scimTestUser
		for i := startIndex; i < startIndex+serverCap && i <= total; i++ {
			res = append(res, scimTestUser{ID: strconv.Itoa(i)})
		}
		return SCIMListResponse[scimTestUser]{Resources: res, TotalResults: total}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL, PageSize: 100, // asked 100, served 50
	})
	require.NoError(t, err)
	assert.Len(t, users, 120, "a server-side page cap must not be mistaken for the last page")
}

// A full page with no totalResults must still be followed — the short-page
// rule must not truncate a genuinely paginated collection.
func TestListSCIMFollowsFullPagesWithoutTotal(t *testing.T) {
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		total := 5
		var res []scimTestUser
		for i := startIndex; i < startIndex+count && i <= total; i++ {
			res = append(res, scimTestUser{ID: strconv.Itoa(i)})
		}
		return SCIMListResponse[scimTestUser]{Resources: res} // no totalResults
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL, PageSize: 2,
	})
	require.NoError(t, err)
	assert.Len(t, users, 5, "pages 1 and 2 are full, page 3 is short and ends the walk")
}

// An empty page is legitimate when the server has already delivered everything
// it promised — that must not be reported as a failure.
func TestListSCIMEmptyPageAfterFullDeliveryIsNotAnError(t *testing.T) {
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		if startIndex == 1 {
			return SCIMListResponse[scimTestUser]{
				Resources:    []scimTestUser{{ID: "1"}, {ID: "2"}},
				TotalResults: 0, // some servers omit the total entirely
			}
		}
		return SCIMListResponse[scimTestUser]{Resources: nil, TotalResults: 0}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{Provider: "test", URL: srv.URL, PageSize: 2})
	require.NoError(t, err)
	assert.Len(t, users, 2)
}

// itemsPerPage is advisory and some servers omit it; the walk must advance on
// the resources actually returned.
func TestListSCIMIgnoresMissingItemsPerPage(t *testing.T) {
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		total := 3
		var res []scimTestUser
		for i := startIndex; i < startIndex+count && i <= total; i++ {
			res = append(res, scimTestUser{ID: strconv.Itoa(i)})
		}
		return SCIMListResponse[scimTestUser]{Resources: res, TotalResults: total, ItemsPerPage: 0}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL, PageSize: 2,
	})
	require.NoError(t, err)
	assert.Len(t, users, 3)
}

func TestListSCIMStopsWhenTotalReached(t *testing.T) {
	var calls atomic.Int32
	srv := scimServer(t, func(startIndex, count int) SCIMListResponse[scimTestUser] {
		calls.Add(1)
		return SCIMListResponse[scimTestUser]{
			Resources:    []scimTestUser{{ID: "1"}, {ID: "2"}},
			TotalResults: 2,
		}
	})
	defer srv.Close()

	c, _ := testClient(t)
	users, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{Provider: "test", URL: srv.URL})
	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.EqualValues(t, 1, calls.Load(), "no extra request once totalResults is satisfied")
}

func TestListSCIMAppliesDecorate(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(SCIMListResponse[scimTestUser]{TotalResults: 0})
	}))
	defer srv.Close()

	c, _ := testClient(t)
	_, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL,
		Decorate: func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") },
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", gotAuth)
}

func TestListSCIMPropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	_, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{Provider: "figma", URL: srv.URL})
	require.Error(t, err)
	assert.True(t, IsUnauthorized(err))
}

func TestListSCIMPreservesExistingQueryParams(t *testing.T) {
	var gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		json.NewEncoder(w).Encode(SCIMListResponse[scimTestUser]{TotalResults: 0})
	}))
	defer srv.Close()

	c, _ := testClient(t)
	_, err := ListSCIM[scimTestUser](context.Background(), c, SCIMPageOptions{
		Provider: "test", URL: srv.URL + "?filter=active+eq+true",
	})
	require.NoError(t, err)
	assert.Equal(t, "active eq true", gotFilter, "vendor-specific query params must survive pagination")
}
