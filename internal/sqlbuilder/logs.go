package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
)

var logOptionalCols = []string{
	"host", "method", "path", "status", "duration_ms",
	"trace_id", "span_id", "request_id", "user_id", "error", "environment",
}

var logCoreCols = []string{TimestampCol, "level", "service", "message"}

var validLevels = map[string]bool{
	"TRACE": true, "DEBUG": true, "INFO": true, "WARN": true, "ERROR": true, "FATAL": true,
}

// Sort orders accepted by SearchLogsRequest.OrderBy.
const (
	OrderByTimestamp = "timestamp"
	OrderByDuration  = "duration_ms"
)

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
	OrderBy         string
	Direction       string
}

func LogsBuild(ctx context.Context, req SearchLogsRequest, res ColumnResolver) (string, []any, error) {
	if res == nil {
		res = FixedColumns(logCoreCols...)
	}
	from, err := stream(req.Stream)
	if err != nil {
		return "", nil, err
	}
	dir := strings.ToUpper(req.Direction)
	switch dir {
	case "":
		dir = "DESC"
	case "ASC", "DESC":
	default:
		return "", nil, fmt.Errorf("invalid direction %q (want asc or desc)", req.Direction)
	}
	orderCol := TimestampCol
	switch req.OrderBy {
	case "", OrderByTimestamp:
	case OrderByDuration:
		orderCol = "duration_ms"
	default:
		return "", nil, fmt.Errorf("invalid order_by %q", req.OrderBy)
	}
	limit := clampLimit(req.Limit, 100)
	rng := req.Range.Normalize()

	cols := pickLogColumns(ctx, res, req.Stream)

	where := rng.Where()
	if req.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escape(req.Service)))
	}
	if req.Level != "" {
		lvl := strings.ToUpper(req.Level)
		if !validLevels[lvl] {
			return "", nil, fmt.Errorf("invalid level %q (want one of TRACE, DEBUG, INFO, WARN, ERROR, FATAL)", req.Level)
		}
		where = append(where, fmt.Sprintf("level = '%s'", lvl))
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
		"SELECT %s FROM %s WHERE %s ORDER BY %s %s LIMIT %d",
		strings.Join(cols, ", "),
		from,
		strings.Join(where, " AND "),
		orderCol,
		dir,
		limit,
	)
	return sql, nil, nil
}

// pickLogColumns selects the core columns plus whichever optional columns the
// stream's schema has. If the schema can't be resolved it degrades to the core set.
func pickLogColumns(ctx context.Context, res ColumnResolver, stream string) []string {
	out := append([]string(nil), logCoreCols...)
	resolved, err := res.ResolveColumns(ctx, stream)
	if err != nil {
		return out
	}
	have := make(map[string]bool, len(resolved))
	for _, c := range resolved {
		have[c] = true
	}
	for _, c := range logOptionalCols {
		if have[c] {
			out = append(out, c)
		}
	}
	return out
}

type AggregateLogsRequest struct {
	Stream  string
	GroupBy string
	Where   []string // trusted, server-built predicates only
	Range   TimeRange
	Limit   int
}

func AggregateLogsBuild(req AggregateLogsRequest) (string, []any, error) {
	if req.GroupBy == "" {
		return "", nil, fmt.Errorf("AggregateLogs: GroupBy is required")
	}
	if !ValidIdent(req.GroupBy) {
		return "", nil, fmt.Errorf("AggregateLogs: invalid GroupBy %q", req.GroupBy)
	}
	from, err := stream(req.Stream)
	if err != nil {
		return "", nil, err
	}
	where := append(req.Range.Normalize().Where(), req.Where...)
	sql := fmt.Sprintf(
		"SELECT %s AS g, count(*) AS c FROM %s WHERE %s GROUP BY %s ORDER BY c DESC LIMIT %d",
		req.GroupBy, from, strings.Join(where, " AND "), req.GroupBy, clampLimit(req.Limit, maxLimit),
	)
	return sql, nil, nil
}
