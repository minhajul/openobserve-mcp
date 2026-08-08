package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TraceSpan represents one span in a trace.
type TraceSpan map[string]any

// SearchTracesRequest searches for spans matching the filters.
type SearchTracesRequest struct {
	Stream     string
	Service    string
	Operation  string
	Status     string // "ok" / "error"
	TraceID    string
	MinSpanDur int // microseconds
	StartTime  time.Time
	EndTime    time.Time
	Limit      int
}

// SearchTracesResponse is the result.
type SearchTracesResponse struct {
	Hits   []TraceSpan `json:"hits"`
	Total  int64       `json:"total"`
	TookMs int         `json:"took_ms"`
	SQL    string      `json:"query_sql"`
}

// SearchTraces returns spans matching the request.
func (c *Client) SearchTraces(ctx context.Context, req SearchTracesRequest) (*SearchTracesResponse, error) {
	if req.Stream == "" {
		req.Stream = "traces"
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.StartTime.IsZero() {
		req.StartTime = time.Now().Add(-1 * time.Hour)
	}
	if req.EndTime.IsZero() {
		req.EndTime = time.Now()
	}

	where := []string{
		fmt.Sprintf("timestamp >= %d", req.StartTime.UnixMicro()),
		fmt.Sprintf("timestamp <= %d", req.EndTime.UnixMicro()),
	}
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

	body := map[string]any{
		"query": map[string]any{
			"sql":        sql,
			"start_time": req.StartTime.UnixMicro(),
			"end_time":   req.EndTime.UnixMicro(),
			"from":       0,
			"size":       req.Limit,
		},
	}

	endpoint := fmt.Sprintf("/api/%s/_search",
		url.PathEscape(c.cfg.OpenObserveOrg),
	)

	resp := &SearchTracesResponse{SQL: sql}
	if err := c.do(ctx, "POST", endpoint, body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetTrace returns all spans for a given trace_id.
func (c *Client) GetTrace(ctx context.Context, stream, traceID string) (*SearchTracesResponse, error) {
	if stream == "" {
		stream = "traces"
	}
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	return c.SearchTraces(ctx, SearchTracesRequest{
		Stream:  stream,
		TraceID: traceID,
		Limit:   500,
	})
}

// IngestSpans sends spans to the traces stream.
func (c *Client) IngestSpans(ctx context.Context, stream string, spans []TraceSpan) error {
	if stream == "" {
		stream = "traces"
	}
	if len(spans) == 0 {
		return nil
	}
	endpoint := fmt.Sprintf("/api/%s/%s/_json",
		url.PathEscape(c.cfg.OpenObserveOrg),
		url.PathEscape(stream),
	)
	return c.do(ctx, "POST", endpoint, spans, nil)
}
