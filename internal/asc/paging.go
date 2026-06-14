package asc

import (
	"context"
	"iter"
	"net/url"
	"strings"
)

// Pages walks a paginated ASC list endpoint via links.next, yielding one Collection[A] per page.
// Package-level function (not method) because Go does not allow generic methods.
//
//	for page, err := range asc.Pages[AppAttributes](ctx, c, "/v1/apps", url.Values{"limit": {"200"}}) {
//	    if err != nil { return err }
//	    for _, app := range page.Data { ... }
//	}
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
			// next embeds its own query; clear q so filter[] values aren't double-merged.
			path = stripBase(next, c.baseURL)
			q = nil

			if err := ctx.Err(); err != nil {
				yield(Collection[A]{}, err)
				return
			}
		}
	}
}

// stripBase removes a leading scheme://host matching base.
// Non-matching URLs pass through unchanged so buildURL rejects them as foreign hosts.
func stripBase(absolute, base string) string {
	if strings.HasPrefix(absolute, base) {
		return absolute[len(base):]
	}
	return absolute
}
