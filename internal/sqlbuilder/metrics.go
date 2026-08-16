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

func MetricsBuild(ctx context.Context, req QueryMetricsRequest) (string, []any, error) {
	if req.Aggregation == "" {
		req.Aggregation = "avg"
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
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
	}
	where = append(where, req.Range.Where()...)
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
	return sql, nil, nil
}
