package openobserve

import (
	"context"

	"github.com/puku/openobserve-mcp/internal/sqlbuilder"
)

type MetricsResponse struct {
	Hits     []map[string]any `json:"hits"`
	Total    int64            `json:"total"`
	TookMs   int              `json:"took_ms"`
	QuerySQL string           `json:"query_sql"`
}

type QueryMetricsRequest = sqlbuilder.QueryMetricsRequest

func (c *Client) QueryMetrics(ctx context.Context, req QueryMetricsRequest) (*MetricsResponse, error) {
	req.Stream = c.resolveStream(KindMetric, req.Stream)
	sql, _, err := sqlbuilder.MetricsBuild(ctx, req)
	if err != nil {
		return nil, err
	}
	body := searchBody(sql, req.Range, 0, req.Limit)
	resp := &MetricsResponse{QuerySQL: sql}
	if err := c.do(ctx, "POST", c.searchEndpoint(), body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) IngestMetrics(ctx context.Context, stream string, entries []map[string]any) error {
	stream = c.resolveStream(KindMetric, stream)
	if len(entries) == 0 {
		return nil
	}
	return c.do(ctx, "POST", c.ingestEndpoint(stream), entries, nil)
}
