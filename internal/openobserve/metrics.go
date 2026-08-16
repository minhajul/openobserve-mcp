package openobserve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type MetricsResponse struct {
	Hits   []map[string]any `json:"hits"`
	TookMs int              `json:"took_ms"`
	SQL    string           `json:"query_sql"`
}

type QueryMetricsRequest struct {
	Stream      string
	MetricName  string
	Aggregation string
	Service     string
	GroupBy     []string
	StartTime   time.Time
	EndTime     time.Time
	Limit       int
}

func (c *Client) QueryMetrics(ctx context.Context, req QueryMetricsRequest) (*MetricsResponse, error) {
	if req.Stream == "" {
		req.Stream = "metrics"
	}
	if req.Aggregation == "" {
		req.Aggregation = "avg"
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

	agg := strings.ToLower(req.Aggregation)
	fn := strings.ToUpper(agg)
	if agg == "p95" || agg == "p99" {
		fn = "approx_percentile(value, " + strings.TrimPrefix(agg, "p") + ")"
	} else {
		fn = fmt.Sprintf("%s(value)", fn)
	}

	groupCols := append([]string(nil), req.GroupBy...)
	selectCols := append(groupCols, fmt.Sprintf("%s AS value", fn))

	where := []string{
		fmt.Sprintf("metric_name = '%s'", escape(req.MetricName)),
		fmt.Sprintf("timestamp >= %d", req.StartTime.UnixMicro()),
		fmt.Sprintf("timestamp <= %d", req.EndTime.UnixMicro()),
	}
	if req.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escape(req.Service)))
	}

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(selectCols, ", "),
		req.Stream,
		strings.Join(where, " AND "),
	)
	if len(groupCols) > 0 {
		sql += fmt.Sprintf(" GROUP BY %s ORDER BY value DESC", strings.Join(groupCols, ", "))
	} else {
		sql += " ORDER BY value DESC"
	}
	sql += fmt.Sprintf(" LIMIT %d", req.Limit)

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

	resp := &MetricsResponse{SQL: sql}
	if err := c.do(ctx, "POST", endpoint, body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) IngestMetrics(ctx context.Context, stream string, entries []map[string]any) error {
	if stream == "" {
		stream = "metrics"
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
