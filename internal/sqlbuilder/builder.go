package sqlbuilder

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TimestampCol is OpenObserve's indexed event-time column (microseconds).
// Search start_time/end_time prune on it, so every query filters and sorts by it.
const TimestampCol = "_timestamp"

const maxLimit = 1000

type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func NowRange(d time.Duration) TimeRange {
	now := time.Now()
	if d <= 0 {
		d = time.Hour
	}
	return TimeRange{Start: now.Add(-d), End: now}
}

// Normalize returns r, or the default last-hour window when either bound is unset.
func (r TimeRange) Normalize() TimeRange {
	if r.Start.IsZero() || r.End.IsZero() {
		return NowRange(0)
	}
	return r
}

func (r TimeRange) Where() []string {
	return []string{
		fmt.Sprintf("%s >= %d", TimestampCol, r.Start.UnixMicro()),
		fmt.Sprintf("%s <= %d", TimestampCol, r.End.UnixMicro()),
	}
}

func escape(v string) string {
	return strings.ReplaceAll(v, `'`, `''`)
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidIdent reports whether s is safe to interpolate as a column or stream name.
func ValidIdent(s string) bool { return identRe.MatchString(s) }

func stream(name string) (string, error) {
	if !ValidIdent(name) {
		return "", fmt.Errorf("invalid stream name %q", name)
	}
	return `"` + name + `"`, nil
}

func clampLimit(n, def int) int {
	if n <= 0 {
		return def
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
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
