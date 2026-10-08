package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
)

type SearchTracesRequest struct {
	Stream    string
	Service   string
	Operation string
	Status    string
	TraceID   string
	// MinSpanDur is in microseconds, the unit of the `duration` column.
	MinSpanDur int
	Range      TimeRange
	Limit      int
}

func TracesBuild(ctx context.Context, req SearchTracesRequest) (string, []any, error) {
	from, err := stream(req.Stream)
	if err != nil {
		return "", nil, err
	}
	limit := clampLimit(req.Limit, 100)
	rng := req.Range.Normalize()

	where := rng.Where()
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

	order := TimestampCol + " DESC"
	if req.TraceID != "" {
		// A single trace reads best root-first, in causal order.
		order = TimestampCol + " ASC"
	}
	sql := fmt.Sprintf(
		"SELECT %s, trace_id, span_id, parent_span_id, service, operation, duration, status FROM %s WHERE %s ORDER BY %s LIMIT %d",
		TimestampCol,
		from,
		strings.Join(where, " AND "),
		order,
		limit,
	)
	return sql, nil, nil
}
