// Package httpclient provides the shared HTTP layer for SaaS provider
// connectors: timeouts, rate-limit handling, bounded retries, and uniform
// error reporting.
//
// Each connector used to hand-roll `&http.Client{}` with no timeout and no
// retry. That meant one unresponsive vendor could hang a whole sync
// indefinitely, and any 429 — routine on SCIM APIs — surfaced as a hard
// failure mid-reconciliation.
package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	defaultBaseDelay  = 500 * time.Millisecond
	maxRetryAfter     = 60 * time.Second
	// maxErrorBody caps how much of an error response is echoed back. Vendor
	// error pages can be HTML megabytes, and they end up in logs.
	maxErrorBody = 2 << 10
)

// Client is a retrying HTTP client shared by all provider connectors.
type Client struct {
	http       *http.Client
	maxRetries int
	baseDelay  time.Duration
	// sleep is injected so tests do not actually wait out the backoff.
	sleep func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// WithMaxRetries caps how many times a retryable failure is re-attempted.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithBaseDelay sets the first backoff interval; it doubles per attempt.
func WithBaseDelay(d time.Duration) Option {
	return func(c *Client) { c.baseDelay = d }
}

// WithHTTPClient replaces the underlying client, for tests or custom transports.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New builds a Client with sane defaults for third-party SaaS APIs.
func New(opts ...Option) *Client {
	c := &Client{
		http:       &http.Client{Timeout: defaultTimeout},
		maxRetries: defaultMaxRetries,
		baseDelay:  defaultBaseDelay,
		sleep:      sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// APIError is a non-2xx response from a provider API.
//
// URL is always redacted before it is stored: some providers (Trello, and any
// API keyed by query string) carry the credential in the query, and this error
// is printed to the console and written to logs.
type APIError struct {
	Provider   string
	StatusCode int
	Body       string
	URL        string
}

// sensitiveParams are query keys whose values must never reach a log line.
var sensitiveParams = []string{"key", "token", "secret", "password", "api_key", "apikey", "access_token", "auth", "signature", "sig"}

// redactURL replaces the value of any credential-bearing query parameter with
// "REDACTED", keeping the rest of the query for diagnostics.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	clone := *u

	// Credentials also travel in userinfo (https://key:token@host/...).
	if clone.User != nil {
		clone.User = url.User("REDACTED")
	}

	if clone.RawQuery != "" {
		q := clone.Query()
		for name := range q {
			lower := strings.ToLower(name)
			for _, s := range sensitiveParams {
				if strings.Contains(lower, s) {
					q.Set(name, "REDACTED")
					break
				}
			}
		}
		clone.RawQuery = q.Encode()
	}

	return clone.String()
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: API error (status %d) for %s: %s", e.Provider, e.StatusCode, e.URL, e.Body)
}

// IsNotFound reports whether the error is a 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports whether the error is a 401 or 403 — usually a missing
// scope, an expired token, or a plan that does not include the API.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
}

// Do executes req with retries, returning the response on success.
//
// Retry policy, deliberately asymmetric by method:
//   - 429 is retried for ANY method: the request was rate-limited, so the
//     server did not act on it.
//   - 5xx is retried only for GET and HEAD. Retrying a failed DELETE or POST
//     risks removing a seat twice or double-inviting, and a 5xx gives no
//     evidence about whether the server applied the change.
//
// The caller owns closing the returned body.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("rewind request body: %w", err)
				}
				req.Body = body
			}
		}

		resp, err := c.http.Do(req.WithContext(ctx))
		if err != nil {
			// A cancelled or expired context is the caller's decision, not a
			// transient fault — surface it immediately.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// A transport error leaves it unknown whether the server acted, so
			// only safe methods are re-attempted.
			if attempt >= c.maxRetries || !isSafeMethod(req.Method) {
				return nil, err
			}
			if werr := c.wait(ctx, attempt, 0); werr != nil {
				return nil, werr
			}
			continue
		}

		if !c.shouldRetry(req.Method, resp.StatusCode) || attempt >= c.maxRetries {
			return resp, nil
		}

		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		drainAndClose(resp.Body)

		if werr := c.wait(ctx, attempt, retryAfter); werr != nil {
			return nil, werr
		}
	}
}

func (c *Client) shouldRetry(method string, status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 && status <= 599 {
		return isSafeMethod(method)
	}
	return false
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead
}

// wait backs off exponentially with jitter, honouring Retry-After when the
// server supplied one.
func (c *Client) wait(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		delay = c.baseDelay * (1 << attempt)
		// Full jitter: without it, every provider goroutine retries in
		// lockstep and re-triggers the same rate limit.
		delay = time.Duration(rand.Int64N(int64(delay)) + int64(delay)/2)
	}
	if delay > maxRetryAfter {
		delay = maxRetryAfter
	}
	return c.sleep(ctx, delay)
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func drainAndClose(body io.ReadCloser) {
	// Draining lets the connection return to the keep-alive pool instead of
	// being torn down on every retry.
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBody))
	_ = body.Close()
}

// DoJSON executes req, checks for a successful status, and decodes the JSON
// body into out. Pass a nil out to discard the body.
func (c *Client) DoJSON(ctx context.Context, provider string, req *http.Request, out any) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("%s: %w", provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &APIError{
			Provider:   provider,
			StatusCode: resp.StatusCode,
			Body:       string(body),
			URL:        redactURL(req.URL),
		}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", provider, err)
	}
	return nil
}
