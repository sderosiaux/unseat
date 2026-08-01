package githubcopilot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	assert.Equal(t, "github-copilot", New("tok", "acme").Name())
}

// The one activity flag in this codebase that is not a judgement call: the API
// carries last_activity_at on every seat. The connector is also read-only by
// construction — it exists to measure, not to unassign a developer mid-task.
func TestCapabilities(t *testing.T) {
	caps := New("tok", "acme").Capabilities()
	assert.True(t, caps.ReportsActivity)
	assert.False(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/orgs/acme/copilot/billing/seats", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))

		// Raw JSON in the shape the live API returns, offsets and all.
		fmt.Fprint(w, `{"total_seats":3,"seats":[
		  {"created_at":"2025-05-12T06:00:49-04:00","last_activity_at":"2026-07-31T14:26:52-04:00",
		   "last_activity_editor":"JetBrains-IU/262","plan_type":"business",
		   "assignee":{"login":"dlefevre","type":"User"}},
		  {"created_at":"2025-06-01T09:00:00Z","last_activity_at":null,"plan_type":"business",
		   "assignee":{"login":"neveractive","type":"User"}},
		  {"created_at":"2025-06-01T09:00:00Z","last_activity_at":"2026-07-01T09:00:00Z",
		   "pending_cancellation_date":"2026-08-31","plan_type":"business",
		   "assignee":{"login":"leaving","type":"User"}}
		]}`)
	}))
	defer server.Close()

	users, err := New("tok", "acme").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	// The login stays bare: core classifies an identifier with no "@" as
	// unresolved, which is honest until an alias maps it to a person.
	assert.Equal(t, "dlefevre", users[0].Email)
	assert.Equal(t, "business", users[0].Role)
	assert.Equal(t, core.StatusActive, users[0].Status)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, 2026, users[0].LastActivityAt.Year())
	// Normalised to UTC: the API returns a local offset.
	assert.Equal(t, 18, users[0].LastActivityAt.Hour(), "14:26 at -04:00 is 18:26 UTC")
	assert.Equal(t, "JetBrains-IU/262", users[0].Metadata["editor"])

	// A seat that has never been used carries no timestamp, and none is invented.
	assert.Nil(t, users[1].LastActivityAt)

	// Already scheduled to end: somebody has dealt with it, so it is not a live
	// seat waiting to be reclaimed.
	assert.Equal(t, core.StatusSuspended, users[2].Status)
}

// Unlike the org audit log, these timestamps are RFC3339 with an offset, not
// milliseconds since epoch. Parsing them with the wrong reader lands them in
// the year 58547.
func TestParseSeatTime(t *testing.T) {
	got, ok := parseSeatTime("2026-07-31T14:26:52-04:00")
	require.True(t, ok)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, "UTC", got.Location().String())

	for _, bad := range []string{"", "1754000000000", "not-a-date"} {
		_, ok := parseSeatTime(bad)
		assert.False(t, ok, bad)
	}
}

func TestListUsersPaginates(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Query().Get("page") == "1" {
			var b strings.Builder
			b.WriteString(`{"total_seats":101,"seats":[`)
			for i := 0; i < seatsPageSize; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{"plan_type":"business","assignee":{"login":"u%d"}}`, i)
			}
			b.WriteString(`]}`)
			fmt.Fprint(w, b.String())
			return
		}
		fmt.Fprint(w, `{"total_seats":101,"seats":[{"plan_type":"business","assignee":{"login":"last"}}]}`)
	}))
	defer server.Close()

	users, err := New("tok", "acme").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	assert.Len(t, users, seatsPageSize+1)
	assert.Equal(t, 2, pages, "a short page ends the walk")
}

// A missing scope and a Copilot-less org both answer 4xx; naming the scope is
// the difference between a two-click fix and a dead end.
func TestListUsersNamesTheScopeOnAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := New("tok", "acme").WithBaseURL(server.URL).ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manage_billing:copilot")
}

// Billing reports the pool size and refuses to invent a rate.
func TestBilling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/acme/copilot/billing", r.URL.Path)
		fmt.Fprint(w, `{"seat_breakdown":{"total":10,"active_this_cycle":1,"inactive_this_cycle":9},
		                "plan_type":"business"}`)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.Equal(t, "business", b.Plan)
	assert.Equal(t, 10, b.BilledSeats)
	assert.Equal(t, 10, b.FilledSeats, "every Copilot seat is assigned by definition")
	// Enterprise agreements differ and the usage report is not a statement of
	// this org's rate, so no price is asserted.
	assert.Zero(t, b.CostPerSeat)
	assert.Empty(t, b.Source)
}

// The cycle counters are a trap. Observed live: a pool whose ten seats had all
// been used within forty-eight hours reported nine "inactive this cycle",
// because the billing cycle had just rolled over. Reporting that as waste would
// have invented two thousand dollars a year.
func TestBillingIgnoresCycleRelativeCounters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"seat_breakdown":{"total":10,"active_this_cycle":1,"inactive_this_cycle":9},
		                "plan_type":"business"}`)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)
	// Nothing derived from active/inactive_this_cycle reaches the report: the
	// only usage signal this connector trusts is per-seat last_activity_at.
	assert.Equal(t, 10, b.FilledSeats)
}

func TestBillingNoCopilotSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"seat_breakdown":{"total":0}}`)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	assert.Nil(t, b, "no subscription is an absence, not an error")
}

func TestWriteOperationsRefused(t *testing.T) {
	p := New("tok", "acme")
	assert.Error(t, p.AddUser(context.Background(), "x", "y"))
	assert.Error(t, p.RemoveUser(context.Background(), "x"))
	assert.Error(t, p.SetRole(context.Background(), "x", "y"))
}
