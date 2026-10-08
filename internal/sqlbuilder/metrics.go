package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
)

type QueryMetricsRequest struct {
	Stream      string
	MetricName  string
	Aggregation string
	Service     string
	GroupBy     []string
	Range       TimeRange
	Limit       int
}

var aggregations = map[string]string{
	"avg":   "avg(value)",
	"sum":   "sum(value)",
	"count": "count(value)",
	"max":   "max(value)",
	"min":   "min(value)",
	"p50":   "approx_percentile_cont(value, 0.5)",
	"p90":   "approx_percentile_cont(value, 0.9)",
	"p95":   "approx_percentile_cont(value, 0.95)",
	"p99":   "approx_percentile_cont(value, 0.99)",
}

func MetricsBuild(ctx context.Context, req QueryMetricsRequest) (string, []any, error) {
	if req.MetricName == "" {
		return "", nil, fmt.Errorf("metric name is required")
	}
	agg := strings.ToLower(req.Aggregation)
	if agg == "" {
		agg = "avg"
	}
	fn, ok := aggregations[agg]
	if !ok {
		return "", nil, fmt.Errorf("unsupported aggregation %q", req.Aggregation)
	}
	from, err := stream(req.Stream)
	if err != nil {
		return "", nil, err
	}
	for _, c := range req.GroupBy {
		if !ValidIdent(c) {
			return "", nil, fmt.Errorf("invalid group_by field %q", c)
		}
	}
	limit := clampLimit(req.Limit, 100)
	rng := req.Range.Normalize()

	selectCols := append(append([]string(nil), req.GroupBy...), fmt.Sprintf("%s AS value", fn))

	where := []string{
		fmt.Sprintf("metric_name = '%s'", escape(req.MetricName)),
	}
	where = append(where, rng.Where()...)
	if req.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escape(req.Service)))
	}

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(selectCols, ", "),
		from,
		strings.Join(where, " AND "),
	)
	if len(req.GroupBy) > 0 {
		sql += fmt.Sprintf(" GROUP BY %s ORDER BY value DESC", strings.Join(req.GroupBy, ", "))
	}
	sql += fmt.Sprintf(" LIMIT %d", limit)
	return sql, nil, nil
}
