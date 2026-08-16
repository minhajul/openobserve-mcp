package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type TraceSpan map[string]any

type SearchTracesRequest struct {
	Stream     string
	Service    string
	Operation  string
	Status     string
	TraceID    string
	MinSpanDur int
	Range      TimeRange
	Limit      int
}

type SearchTracesResponse struct {
	Hits   []TraceSpan `json:"hits"`
	Total  int64       `json:"total"`
	TookMs int         `json:"took_ms"`
	SQL    string      `json:"query_sql"`
}

func (c *Client) SearchTraces(ctx context.Context, req SearchTracesRequest) (*SearchTracesResponse, error) {
	req.Stream = c.resolveStream(KindTrace, req.Stream)
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
	}

	where := req.Range.Where()
	if req.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escape(req.Service)))
	}
	if req.Operation != "" {
		where = append(where, fmt.Sprintf("operation = '%s'", escape(req.Operation)))
	}
	if req.Status != "" {
		where = append(where, fmt.Sprintf("status = '%s'", escape(strings.ToLower(req.Status))))
	}
	if req.TraceID != "" {
		where = append(where, fmt.Sprintf("trace_id = '%s'", escape(req.TraceID)))
	}
	if req.MinSpanDur > 0 {
		where = append(where, fmt.Sprintf("duration >= %d", req.MinSpanDur))
	}

	sql := fmt.Sprintf(
		"SELECT trace_id, span_id, parent_span_id, service, operation, duration, status, timestamp FROM %s WHERE %s ORDER BY timestamp DESC LIMIT %d",
		req.Stream,
		strings.Join(where, " AND "),
		req.Limit,
	)

	body := searchBody(sql, req.Range, 0, req.Limit)
	resp := &SearchTracesResponse{SQL: sql}
	if err := c.do(ctx, "POST", searchEndpoint(c.cfg.OpenObserveOrg), body, resp); err != nil {
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
	endpoint := fmt.Sprintf("/api/%s/%s/_json",
		url.PathEscape(c.cfg.OpenObserveOrg),
		url.PathEscape(stream),
	)
	return c.do(ctx, "POST", endpoint, spans, nil)
}
