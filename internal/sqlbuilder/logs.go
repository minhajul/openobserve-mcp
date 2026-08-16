package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
)

var logOptionalCols = []string{"host", "method", "path", "status", "duration_ms", "trace_id", "error", "environment"}

var logCoreCols = []string{"timestamp", "level", "service", "message"}

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

func LogsBuild(ctx context.Context, req SearchLogsRequest, res ColumnResolver) (string, []any, error) {
	if res == nil {
		res = FixedColumns(logCoreCols...)
	}
	if req.Direction == "" {
		req.Direction = "desc"
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
	}

	cols, err := pickLogColumns(ctx, res, req.Stream)
	if err != nil {
		return "", nil, err
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

	sql := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY timestamp %s LIMIT %d",
		strings.Join(cols, ", "),
		req.Stream,
		strings.Join(where, " AND "),
		strings.ToLower(req.Direction),
		req.Limit,
	)
	return sql, nil, nil
}

func pickLogColumns(ctx context.Context, res ColumnResolver, stream string) ([]string, error) {
	resolved, err := res.ResolveColumns(ctx, stream)
	if err != nil {
		return []string{"timestamp"}, nil
	}
	have := make(map[string]bool, len(resolved))
	for _, c := range resolved {
		have[c] = true
	}
	out := append([]string(nil), logCoreCols...)
	for _, c := range logOptionalCols {
		if have[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

type AggregateLogsRequest struct {
	Stream  string
	GroupBy string
	Where   []string
	Range   TimeRange
}

func AggregateLogsBuild(req AggregateLogsRequest) (string, []any, error) {
	if req.GroupBy == "" {
		return "", nil, fmt.Errorf("AggregateLogs: GroupBy is required")
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
	return sql, nil, nil
}
