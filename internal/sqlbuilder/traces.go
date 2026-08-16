package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
)

type SearchTracesRequest struct {
	Stream     string
	Service    string
	Operation  string
	Status     string
	TraceID    string
	MinSpanDur int
	Range      TimeRange
	Limit      int
}

func TracesBuild(ctx context.Context, req SearchTracesRequest) (string, []any, error) {
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Range.Start.IsZero() || req.Range.End.IsZero() {
		req.Range = NowRange(0)
	}

	where := req.Range.Where()
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
	return sql, nil, nil
}
