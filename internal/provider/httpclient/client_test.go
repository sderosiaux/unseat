package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClient never really sleeps: backoff correctness is asserted through the
// recorded delays instead of by waiting them out.
func testClient(t *testing.T, opts ...Option) (*Client, *[]time.Duration) {
	t.Helper()
	var delays []time.Duration
	c := New(opts...)
	c.sleep = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}
	return c, &delays
}

func get(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	return req
}

func TestDoJSONDecodesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprint(w, `{"name":"alice"}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	var out struct {
		Name string `json:"name"`
	}
	require.NoError(t, c.DoJSON(context.Background(), "test", get(t, srv.URL), &out))
	assert.Equal(t, "alice", out.Name)
}

func TestDoJSONReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, err := fmt.Fprint(w, `{"error":"plan does not include SCIM"}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	err := c.DoJSON(context.Background(), "slack", get(t, srv.URL), nil)
	require.Error(t, err)

	assert.True(t, IsUnauthorized(err))
	assert.False(t, IsNotFound(err))
	assert.Contains(t, err.Error(), "slack")
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "plan does not include SCIM")
}

func TestRetriesOn429ForAnyMethod(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, err := fmt.Fprint(w, `{}`)
				require.NoError(t, err)
			}))
			defer srv.Close()

			c, _ := testClient(t)
			req, err := http.NewRequest(method, srv.URL, nil)
			require.NoError(t, err)

			// A 429 means the server did not act on the request, so retrying
			// is safe even for a destructive method.
			require.NoError(t, c.DoJSON(context.Background(), "test", req, nil))
			assert.EqualValues(t, 2, calls.Load())
		})
	}
}

// A 5xx gives no evidence about whether the server applied the change, so a
// removal must not be replayed — that would delete a seat twice.
func TestDoesNotRetry5xxOnUnsafeMethods(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	req, err := http.NewRequest(http.MethodDelete, srv.URL, nil)
	require.NoError(t, err)

	err = c.DoJSON(context.Background(), "test", req, nil)
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load(), "a failed DELETE must not be replayed")
}

func TestRetries5xxOnGet(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, err := fmt.Fprint(w, `{}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	require.NoError(t, c.DoJSON(context.Background(), "test", get(t, srv.URL), nil))
	assert.EqualValues(t, 3, calls.Load())
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, _ := testClient(t, WithMaxRetries(2))
	err := c.DoJSON(context.Background(), "test", get(t, srv.URL), nil)
	require.Error(t, err)
	assert.EqualValues(t, 3, calls.Load(), "initial attempt plus 2 retries")
}

func TestHonoursRetryAfterHeader(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := fmt.Fprint(w, `{}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, delays := testClient(t)
	require.NoError(t, c.DoJSON(context.Background(), "test", get(t, srv.URL), nil))
	require.Len(t, *delays, 1)
	assert.Equal(t, 7*time.Second, (*delays)[0], "the server's own backoff must win over ours")
}

func TestRetryAfterIsCapped(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "99999")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := fmt.Fprint(w, `{}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, delays := testClient(t)
	require.NoError(t, c.DoJSON(context.Background(), "test", get(t, srv.URL), nil))
	require.Len(t, *delays, 1)
	assert.Equal(t, maxRetryAfter, (*delays)[0], "a hostile Retry-After must not stall the sync for a day")
}

func TestRetryReplaysRequestBody(t *testing.T) {
	var bodies []string
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := fmt.Fprint(w, `{}`)
		require.NoError(t, err)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"email":"a@co.com"}`))
	require.NoError(t, err)

	require.NoError(t, c.DoJSON(context.Background(), "test", req, nil))
	require.Len(t, bodies, 2)
	assert.Equal(t, bodies[0], bodies[1], "a retried request must resend its body, not an empty one")
}

func TestContextCancellationIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := New()
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	err := c.DoJSON(ctx, "test", get(t, srv.URL), nil)
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load())
}

func TestTimeoutIsSet(t *testing.T) {
	assert.Equal(t, defaultTimeout, New().http.Timeout,
		"a provider connector without a timeout can hang a whole sync")
	assert.Equal(t, 5*time.Second, New(WithTimeout(5*time.Second)).http.Timeout)
}

// Trello and similar APIs put the credential in the query string. This error
// is printed to the console and written to logs, so the secret must not be in it.
func TestAPIErrorRedactsCredentialsInURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := testClient(t)
	req := get(t, srv.URL+"/1/organizations/acme/members?key=SUPERSECRETKEY&token=SUPERSECRETTOKEN&fields=all")

	err := c.DoJSON(context.Background(), "trello", req, nil)
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "SUPERSECRETKEY")
	assert.NotContains(t, err.Error(), "SUPERSECRETTOKEN")
	assert.Contains(t, err.Error(), "REDACTED")
	// Non-sensitive parameters stay, so the error is still diagnosable.
	assert.Contains(t, err.Error(), "fields=all")
}

func TestRedactURL(t *testing.T) {
	cases := map[string]struct{ in, mustNotContain, mustContain string }{
		"api_key":        {"https://api.co/u?api_key=abc123&page=2", "abc123", "page=2"},
		"access_token":   {"https://api.co/u?access_token=xyz789", "xyz789", "REDACTED"},
		"mixed case":     {"https://api.co/u?ApiKey=Secret1", "Secret1", "REDACTED"},
		"basic userinfo": {"https://user:pass@api.co/u", "pass", "REDACTED"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			u, err := url.Parse(tc.in)
			require.NoError(t, err)
			got := redactURL(u)
			assert.NotContains(t, got, tc.mustNotContain)
			assert.Contains(t, got, tc.mustContain)
		})
	}

	t.Run("no query is untouched", func(t *testing.T) {
		u, _ := url.Parse("https://api.co/scim/v2/Users")
		assert.Equal(t, "https://api.co/scim/v2/Users", redactURL(u))
	})
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, 5*time.Second, parseRetryAfter("5"))
	assert.Zero(t, parseRetryAfter(""))
	assert.Zero(t, parseRetryAfter("garbage"))
	assert.Zero(t, parseRetryAfter("-3"))
	// An HTTP-date in the past means "retry now", not "sleep backwards".
	assert.Zero(t, parseRetryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)))
	assert.Positive(t, parseRetryAfter(time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat)))
}
