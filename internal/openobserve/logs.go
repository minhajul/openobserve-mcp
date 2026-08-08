package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// LogEntry is a single log record. OpenObserve uses a JSON map internally,
// but the MCP layer benefits from typed fields.
type LogEntry map[string]any

// SearchLogsRequest is the typed input the MCP layer hands to the client.
type SearchLogsRequest struct {
	// Stream is the OpenObserve stream name (e.g. "default").
	Stream string
	// Service is the value of the `service` field, if any.
	Service string
	// Level is the value of the `level` field, e.g. "ERROR", "INFO".
	Level string
	// Status filters by the `status` field (e.g. "500").
	Status string
	// TraceID filters by `trace_id`.
	TraceID string
	// MessageContains performs a LIKE match on `message`.
	MessageContains string
	// MinDurationMS filters to entries with `duration_ms >= MinDurationMS`.
	MinDurationMS int
	// StartTime / EndTime bound the search window.
	StartTime time.Time
	EndTime   time.Time
	// Limit caps the number of returned rows (default 100, max 1000).
	Limit int
	// Direction is "asc" or "desc" (default "desc").
	Direction string
}

// SearchLogsResponse is the result of a search.
type SearchLogsResponse struct {
	Hits     []LogEntry `json:"hits"`
	Total    int64      `json:"total"`
	TookMs   int        `json:"took_ms"`
	QuerySQL string     `json:"query_sql"`
}

// SearchLogs executes a structured log search against OpenObserve.
func (c *Client) SearchLogs(ctx context.Context, req SearchLogsRequest) (*SearchLogsResponse, error) {
	if req.Stream == "" {
		req.Stream = "default"
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}
	if req.Direction == "" {
		req.Direction = "desc"
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
	if req.Level != "" {
		where = append(where, fmt.Sprintf("level = '%s'", escape(strings.ToUpper(req.Level))))
	}
	if req.Status != "" {
		where = append(where, fmt.Sprintf("status = '%s'", escape(req.Status)))
	}
	if req.TraceID != "" {
		where = append(where, fmt.Sprintf("trace_id = '%s'", escape(req.TraceID)))
	}
	if req.MessageContains != "" {
		where = append(where, fmt.Sprintf("message ILIKE '%%%s%%'", escape(req.MessageContains)))
	}
	if req.MinDurationMS > 0 {
		where = append(where, fmt.Sprintf("duration_ms >= %d", req.MinDurationMS))
	}

	// Discover the columns present in the stream's schema. We must avoid
	// SELECT-ing a field that does not exist (OpenObserve errors out).
	// We probe by hitting the streams endpoint and filtering to fields
	// the stream has registered.
	selectCols := []string{"timestamp", "level", "service", "message"}
	if schema, err := c.streamSchema(ctx, req.Stream); err == nil {
		for _, col := range []string{"host", "method", "path", "status", "duration_ms", "trace_id", "error", "environment"} {
			if schema[col] {
				selectCols = append(selectCols, col)
			}
		}
	} else {
		// Fallback to a minimal projection when the schema lookup fails.
		selectCols = []string{"timestamp"}
	}

	sql := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY timestamp %s LIMIT %d",
		strings.Join(selectCols, ", "),
		req.Stream,
		strings.Join(where, " AND "),
		strings.ToLower(req.Direction),
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

	// OpenObserve's search endpoint lives at /api/{org}/_search (no
	// stream in path); the stream is referenced inside the SQL.
	endpoint := fmt.Sprintf("/api/%s/_search",
		url.PathEscape(c.cfg.OpenObserveOrg),
	)

	resp := &SearchLogsResponse{QuerySQL: sql}
	if err := c.do(ctx, "POST", endpoint, body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// IngestLogs sends a batch of log records to OpenObserve using the
// multi-stream JSON ingestion endpoint.
//
// OpenObserve's ingestion endpoint expects a top-level JSON array, e.g.:
//
//	POST /api/{org}/{stream}/_json
//	[{"timestamp":...}, {"timestamp":...}]
//
// See: https://openobserve.ai/docs/api/ingestion/
func (c *Client) IngestLogs(ctx context.Context, stream string, entries []LogEntry) error {
	if stream == "" {
		stream = "default"
	}
	if len(entries) == 0 {
		return nil
	}
	endpoint := fmt.Sprintf("/api/%s/%s/_json",
		url.PathEscape(c.cfg.OpenObserveOrg),
		url.PathEscape(stream),
	)
	return c.do(ctx, "POST", endpoint, entries, nil)
}

// AggregateLogsRequest is the input for a SQL aggregation that returns
// counts grouped by a single column.
type AggregateLogsRequest struct {
	Stream    string
	GroupBy   string   // column to group by (e.g. "service", "status")
	Where     []string // extra WHERE clauses (joined with AND)
	StartTime time.Time
	EndTime   time.Time
}

// AggregateLogsResponse is the result of a GROUP BY count.
type AggregateLogsResponse struct {
	Groups   map[string]int64 `json:"groups"`
	Total    int64            `json:"total"`
	QuerySQL string           `json:"query_sql"`
}

// AggregateLogs runs `SELECT group_col, count(*) FROM stream WHERE ...
// GROUP BY group_col` and returns the counts. The total matches the sum
// of the returned group counts because OpenObserve executes the
// aggregation server-side rather than sampling rows in Go.
func (c *Client) AggregateLogs(ctx context.Context, req AggregateLogsRequest) (*AggregateLogsResponse, error) {
	if req.Stream == "" {
		req.Stream = "default"
	}
	if req.GroupBy == "" {
		return nil, fmt.Errorf("AggregateLogs: GroupBy is required")
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
	where = append(where, req.Where...)

	col := escape(req.GroupBy)
	sql := fmt.Sprintf(
		"SELECT %s AS g, count(*) AS c FROM %s WHERE %s GROUP BY %s",
		col, req.Stream, strings.Join(where, " AND "), col,
	)

	body := map[string]any{
		"query": map[string]any{
			"sql":        sql,
			"start_time": req.StartTime.UnixMicro(),
			"end_time":   req.EndTime.UnixMicro(),
			"from":       0,
			"size":       1000,
		},
	}
	endpoint := fmt.Sprintf("/api/%s/_search", url.PathEscape(c.cfg.OpenObserveOrg))

	var raw struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := c.do(ctx, "POST", endpoint, body, &raw); err != nil {
		return nil, err
	}
	out := &AggregateLogsResponse{
		Groups:   make(map[string]int64, len(raw.Hits)),
		QuerySQL: sql,
	}
	for _, h := range raw.Hits {
		g, _ := h["g"].(string)
		count, _ := toInt64(h["c"])
		out.Groups[g] = count
		out.Total += count
	}
	return out, nil
}

// toInt64 coerces a JSON number (always float64 from encoding/json) to
// int64. Returns 0 on type mismatch.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}
