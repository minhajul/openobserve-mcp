package sqlbuilder

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type TimeRange struct {
	Start time.Time
	End   time.Time
}

func NowRange(d time.Duration) TimeRange {
	now := time.Now()
	if d <= 0 {
		d = time.Hour
	}
	return TimeRange{Start: now.Add(-d), End: now}
}

func (r TimeRange) Where() []string {
	return []string{
		fmt.Sprintf("timestamp >= %d", r.Start.UnixMicro()),
		fmt.Sprintf("timestamp <= %d", r.End.UnixMicro()),
	}
}

func escape(v string) string {
	return strings.ReplaceAll(v, `'`, `''`)
}

type ColumnResolver interface {
	ResolveColumns(ctx context.Context, stream string) ([]string, error)
}

type fixedColumns struct{ cols []string }

func (f fixedColumns) ResolveColumns(ctx context.Context, stream string) ([]string, error) {
	out := make([]string, len(f.cols))
	copy(out, f.cols)
	return out, nil
}

func FixedColumns(cols ...string) ColumnResolver {
	return fixedColumns{cols: cols}
}

type built struct {
	SQL  string
	Args []any
}
