package asc

import (
	"context"
	"iter"
	"net/url"
	"strings"
)

// Pages walks a paginated ASC list endpoint via Apple's links.next URLs.
// It yields one Collection[A] per page; on error the iterator yields the
// zero-value collection plus the error and stops.
//
// Usage:
//
//	for page, err := range asc.Pages[AppAttributes](ctx, c, "/v1/apps", url.Values{"limit": {"200"}}) {
//	    if err != nil { return err }
//	    for _, app := range page.Data {
//	        // ...
//	    }
//	}
//
// Pages is a package-level function (not a method) because Go does not allow
// generic methods on a non-generic type.
func Pages[A any](ctx context.Context, c *Client, firstPath string, query url.Values) iter.Seq2[Collection[A], error] {
	return func(yield func(Collection[A], error) bool) {
		path := firstPath
		q := query

		for {
			page, err := Get[Collection[A]](ctx, c, path, q)
			if err != nil {
				yield(Collection[A]{}, err)
				return
			}
			if !yield(page, nil) {
				return
			}
			next := page.Links.Next
			if next == "" {
				return
			}
			// Subsequent pages: follow links.next as-is. Strip the base host
			// when present so do() doesn't re-parse a foreign host. The query
			// is already embedded in next's URL — clear the explicit query map
			// so we don't double-merge filter[] values.
			path = stripBase(next, c.baseURL)
			q = nil

			// Best-effort cancellation between pages.
			if err := ctx.Err(); err != nil {
				yield(Collection[A]{}, err)
				return
			}
		}
	}
}

// stripBase returns the URL with the leading scheme://host removed when it
// matches base. If it doesn't match, the original URL is returned unchanged
// and buildURL will reject it as a foreign host.
func stripBase(absolute, base string) string {
	if strings.HasPrefix(absolute, base) {
		return absolute[len(base):]
	}
	return absolute
}
