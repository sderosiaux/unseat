package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// SCIMListResponse is the envelope every SCIM v2 list endpoint returns.
type SCIMListResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	Resources    []T      `json:"Resources"`
	TotalResults int      `json:"totalResults"`
	ItemsPerPage int      `json:"itemsPerPage"`
	StartIndex   int      `json:"startIndex"`
}

const (
	defaultSCIMPageSize = 100
	// maxSCIMPages bounds the walk regardless of what the server reports.
	// A vendor that keeps claiming more results than it returns cannot turn
	// a sync into an infinite loop.
	maxSCIMPages = 1000
)

// SCIMPageOptions configures a SCIM collection walk.
type SCIMPageOptions struct {
	// Provider names the connector, for error messages.
	Provider string
	// URL is the collection endpoint, e.g. https://api.slack.com/scim/v2/Users.
	URL string
	// PageSize is the requested count per page; defaults to 100.
	PageSize int
	// Decorate applies auth headers and anything else the vendor requires.
	Decorate func(*http.Request)
}

// ListSCIM walks a paginated SCIM v2 collection and returns every resource.
//
// Termination is guarded three ways, because the hand-rolled version of this
// loop — copied across seven connectors — advanced startIndex by the number of
// resources returned and so spun forever whenever a server returned an empty
// page while still claiming more results:
//   - an empty page ends the walk, whatever totalResults says
//   - the walk ends once the accumulated count reaches totalResults
//   - a hard page cap ends it regardless
func ListSCIM[T any](ctx context.Context, c *Client, opts SCIMPageOptions) ([]T, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultSCIMPageSize
	}

	base, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid SCIM URL %q: %w", opts.Provider, opts.URL, err)
	}

	var all []T
	startIndex := 1

	for page := 0; page < maxSCIMPages; page++ {
		u := *base
		q := u.Query()
		q.Set("startIndex", fmt.Sprintf("%d", startIndex))
		q.Set("count", fmt.Sprintf("%d", pageSize))
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		if opts.Decorate != nil {
			opts.Decorate(req)
		}

		var resp SCIMListResponse[T]
		if err := c.DoJSON(ctx, opts.Provider, req, &resp); err != nil {
			return nil, err
		}

		if len(resp.Resources) == 0 {
			// The server owes us more results and delivered none. Returning
			// the partial list would be worse than failing: the caller caches
			// seats with DELETE-then-INSERT, so a truncated inventory silently
			// replaces the real one, and every missing user then looks absent
			// from the SaaS and gets re-invited.
			if resp.TotalResults > len(all) {
				return nil, fmt.Errorf(
					"%s: SCIM pagination stalled at %d of %d reported results — the API returned an empty page while claiming more",
					opts.Provider, len(all), resp.TotalResults)
			}
			return all, nil
		}

		all = append(all, resp.Resources...)

		if resp.TotalResults > 0 && len(all) >= resp.TotalResults {
			return all, nil
		}

		// A short page ends the walk only when the server is not promising
		// more. RFC 7644 makes totalResults mandatory but servers omit it, and
		// without this a server that ignores startIndex is walked to the page
		// cap — a thousand live calls ending in an error, where the previous
		// hand-rolled loop simply stopped.
		//
		// When more IS promised, a short page means the server capped the page
		// size below what we asked for, which is common and legitimate, so the
		// walk continues.
		if len(resp.Resources) < pageSize && resp.TotalResults <= len(all) {
			return all, nil
		}

		startIndex += len(resp.Resources)
	}

	return all, fmt.Errorf("%s: SCIM pagination exceeded %d pages — the API keeps reporting more results than it returns", opts.Provider, maxSCIMPages)
}
