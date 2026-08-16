package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
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
	Range           TimeRange
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
	req.Stream = c.resolveStream(KindLog, req.Stream)
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}
	if req.Direction == "" {
		req.Direction = "desc"
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
	}

	where := req.Range.Where()
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

	body := searchBody(sql, req.Range, 0, req.Limit)
	resp := &SearchLogsResponse{QuerySQL: sql}
	if err := c.do(ctx, "POST", searchEndpoint(c.cfg.OpenObserveOrg), body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) IngestLogs(ctx context.Context, stream string, entries []LogEntry) error {
	stream = c.resolveStream(KindLog, stream)
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
	Stream  string
	GroupBy string
	Where   []string
	Range   TimeRange
}

type AggregateLogsResponse struct {
	Groups   map[string]int64 `json:"groups"`
	Total    int64            `json:"total"`
	QuerySQL string           `json:"query_sql"`
}

func (c *Client) AggregateLogs(ctx context.Context, req AggregateLogsRequest) (*AggregateLogsResponse, error) {
	req.Stream = c.resolveStream(KindLog, req.Stream)
	if req.GroupBy == "" {
		return nil, fmt.Errorf("AggregateLogs: GroupBy is required")
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
	}

	where := append(req.Range.Where(), req.Where...)

	col := escape(req.GroupBy)
	sql := fmt.Sprintf(
		"SELECT %s AS g, count(*) AS c FROM %s WHERE %s GROUP BY %s",
		col, req.Stream, strings.Join(where, " AND "), col,
	)

	body := searchBody(sql, req.Range, 0, 1000)
	var raw struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := c.do(ctx, "POST", searchEndpoint(c.cfg.OpenObserveOrg), body, &raw); err != nil {
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
