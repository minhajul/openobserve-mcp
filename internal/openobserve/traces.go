package openobserve

import (
	"context"
	"fmt"

	"github.com/puku/openobserve-mcp/internal/sqlbuilder"
)

type TraceSpan map[string]any

type SearchTracesRequest = sqlbuilder.SearchTracesRequest

type SearchTracesResponse struct {
	Hits   []TraceSpan `json:"hits"`
	Total  int64       `json:"total"`
	TookMs int         `json:"took_ms"`
	SQL    string      `json:"query_sql"`
}

func (c *Client) SearchTraces(ctx context.Context, req SearchTracesRequest) (*SearchTracesResponse, error) {
	req.Stream = c.resolveStream(KindTrace, req.Stream)
	sql, _, err := sqlbuilder.TracesBuild(ctx, req)
	if err != nil {
		return nil, err
	}
	body := searchBody(sql, req.Range, 0, req.Limit)
	resp := &SearchTracesResponse{SQL: sql}
	if err := c.do(ctx, "POST", c.searchEndpoint(), body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetTrace(ctx context.Context, stream, traceID string) (*SearchTracesResponse, error) {
	stream = c.resolveStream(KindTrace, stream)
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	return c.SearchTraces(ctx, SearchTracesRequest{
		Stream:  stream,
		TraceID: traceID,
		Limit:   500,
	})
}

func (c *Client) IngestSpans(ctx context.Context, stream string, spans []TraceSpan) error {
	stream = c.resolveStream(KindTrace, stream)
	if len(spans) == 0 {
		return nil
	}
	return c.do(ctx, "POST", c.ingestEndpoint(stream), spans, nil)
}
