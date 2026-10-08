package openobserve

import (
	"context"

	"github.com/puku/openobserve-mcp/internal/sqlbuilder"
)

type MetricsResponse struct {
	Hits     []map[string]any `json:"hits"`
	Count    int              `json:"count"`
	TookMs   int              `json:"took_ms"`
	QuerySQL string           `json:"-"`
}

type QueryMetricsRequest = sqlbuilder.QueryMetricsRequest

func (c *Client) QueryMetrics(ctx context.Context, req QueryMetricsRequest) (*MetricsResponse, error) {
	req.Stream = c.resolveStream(KindMetric, req.Stream)
	req.Range = req.Range.Normalize()
	sql, _, err := sqlbuilder.MetricsBuild(ctx, req)
	if err != nil {
		return nil, err
	}
	res, err := c.search(ctx, sql, req.Range)
	if err != nil {
		return nil, err
	}
	return &MetricsResponse{Hits: res.Hits, Count: len(res.Hits), TookMs: res.Took, QuerySQL: sql}, nil
}

func (c *Client) IngestMetrics(ctx context.Context, stream string, entries []map[string]any) error {
	stream = c.resolveStream(KindMetric, stream)
	if len(entries) == 0 {
		return nil
	}
	return c.do(ctx, "POST", c.ingestEndpoint(stream), entries, nil)
}
