package openobserve

import (
	"context"
)

// searchResult is the subset of OpenObserve's /_search response we consume.
// Note: upstream "total" is the number of hits returned, not the match count.
type searchResult struct {
	Hits []map[string]any `json:"hits"`
	Took int              `json:"took"`
}

// searchPageSize is the upstream page size. It equals the builders' max LIMIT,
// so the SQL LIMIT is always what bounds the result.
const searchPageSize = 1000

func (c *Client) search(ctx context.Context, sql string, rng TimeRange) (*searchResult, error) {
	var out searchResult
	if err := c.do(ctx, "POST", c.searchEndpoint(), searchBody(sql, rng, 0, searchPageSize), &out); err != nil {
		return nil, err
	}
	humanizeTimestamps(out.Hits)
	return &out, nil
}
