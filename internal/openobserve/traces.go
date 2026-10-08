package openobserve

import (
	"context"
	"fmt"
	"time"

	"github.com/puku/openobserve-mcp/internal/sqlbuilder"
)

// traceLookupWindow is how far back GetTrace searches when no range is given:
// a trace ID is a precise key, so a wide window costs little and avoids misses.
const traceLookupWindow = 7 * 24 * time.Hour

type TraceSpan map[string]any

type SearchTracesRequest = sqlbuilder.SearchTracesRequest

type SearchTracesResponse struct {
	Hits     []TraceSpan `json:"hits"`
	Count    int         `json:"count"`
	TookMs   int         `json:"took_ms"`
	QuerySQL string      `json:"-"`
}

func (c *Client) SearchTraces(ctx context.Context, req SearchTracesRequest) (*SearchTracesResponse, error) {
	req.Stream = c.resolveStream(KindTrace, req.Stream)
	req.Range = req.Range.Normalize()
	sql, _, err := sqlbuilder.TracesBuild(ctx, req)
	if err != nil {
		return nil, err
	}
	res, err := c.search(ctx, sql, req.Range)
	if err != nil {
		return nil, err
	}
	resp := &SearchTracesResponse{Hits: make([]TraceSpan, len(res.Hits)), Count: len(res.Hits), TookMs: res.Took, QuerySQL: sql}
	for i, h := range res.Hits {
		resp.Hits[i] = h
	}
	return resp, nil
}

// GetTrace returns every span of traceID. A zero rng searches the last 7 days.
func (c *Client) GetTrace(ctx context.Context, stream, traceID string, rng TimeRange) (*SearchTracesResponse, error) {
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	if rng.Start.IsZero() || rng.End.IsZero() {
		rng = NowRange(traceLookupWindow)
	}
	return c.SearchTraces(ctx, SearchTracesRequest{
		Stream:  stream,
		TraceID: traceID,
		Limit:   1000,
		Range:   rng,
	})
}

func (c *Client) IngestSpans(ctx context.Context, stream string, spans []TraceSpan) error {
	stream = c.resolveStream(KindTrace, stream)
	if len(spans) == 0 {
		return nil
	}
	return c.do(ctx, "POST", c.ingestEndpoint(stream), spans, nil)
}
