package state

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ul0gic/flightline/internal/asc"
)

// Resolve only after reading the complete collection: an incomplete scan cannot authorize creation.
func uniqueAssetResource[A any](ctx context.Context, c *asc.Client, path string, query url.Values, match func(asc.Resource[A]) bool) (string, error) {
	var id string
	for page, err := range asc.Pages[A](ctx, c, path, query) {
		if err != nil {
			return "", err
		}
		for _, row := range page.Data {
			if !match(row) {
				continue
			}
			if row.ID == "" {
				return "", fmt.Errorf("asset lookup %s returned matching resource without ID", path)
			}
			if id != "" {
				return "", fmt.Errorf("asset lookup %s is ambiguous", path)
			}
			id = row.ID
		}
	}
	return id, nil
}
