package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

const maxPaginationPages = 100

// ListAll follows the API's page_token/next_cursor contract with a hard
// ceiling and cursor-loop detection so a bad server cursor cannot hang a plan.
func ListAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	endpoint, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse paginated endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("page_size", "200")
	seen := make(map[string]struct{})
	var items []T
	for pageNumber := 0; pageNumber < maxPaginationPages; pageNumber++ {
		endpoint.RawQuery = query.Encode()
		var page Page[T]
		if err := c.Do(ctx, http.MethodGet, endpoint.String(), nil, &page, ""); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			return items, nil
		}
		if _, duplicate := seen[page.NextCursor]; duplicate {
			return nil, fmt.Errorf("pagination cursor %q repeated", page.NextCursor)
		}
		seen[page.NextCursor] = struct{}{}
		query.Set("page_token", page.NextCursor)
	}
	return nil, fmt.Errorf("pagination exceeded %d pages", maxPaginationPages)
}
