package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type LogEntry map[string]any

type SearchLogsRequest struct {
	Stream          string
	Service         string
	Level           string
	Status          string
	TraceID         string
	MessageContains string
	MinDurationMS   int
	StartTime       time.Time
	EndTime         time.Time
	Limit           int
	Direction       string
}

type SearchLogsResponse struct {
	Hits     []LogEntry `json:"hits"`
	Total    int64      `json:"total"`
	TookMs   int        `json:"took_ms"`
	QuerySQL string     `json:"query_sql"`
}

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

	selectCols := []string{"timestamp", "level", "service", "message"}
	if schema, err := c.streamSchema(ctx, req.Stream); err == nil {
		for _, col := range []string{"host", "method", "path", "status", "duration_ms", "trace_id", "error", "environment"} {
			if schema[col] {
				selectCols = append(selectCols, col)
			}
		}
	} else {
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

	endpoint := fmt.Sprintf("/api/%s/_search",
		url.PathEscape(c.cfg.OpenObserveOrg),
	)

	resp := &SearchLogsResponse{QuerySQL: sql}
	if err := c.do(ctx, "POST", endpoint, body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

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

type AggregateLogsRequest struct {
	Stream    string
	GroupBy   string
	Where     []string
	StartTime time.Time
	EndTime   time.Time
}

type AggregateLogsResponse struct {
	Groups   map[string]int64 `json:"groups"`
	Total    int64            `json:"total"`
	QuerySQL string           `json:"query_sql"`
}

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
