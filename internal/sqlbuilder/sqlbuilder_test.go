package sqlbuilder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTimeRangeNowRange(t *testing.T) {
	before := time.Now()
	r := NowRange(0)
	if r.End.Before(before) || r.End.After(time.Now()) {
		t.Fatalf("End %v outside now", r.End)
	}
	if r.End.Sub(r.Start) != time.Hour {
		t.Fatalf("default = %v, want 1h", r.End.Sub(r.Start))
	}
}

func TestTimeRangeWhere(t *testing.T) {
	r := TimeRange{
		Start: time.UnixMicro(1_000_000),
		End:   time.UnixMicro(2_000_000),
	}
	w := r.Where()
	if len(w) != 2 || w[0] != "timestamp >= 1000000" || w[1] != "timestamp <= 2000000" {
		t.Errorf("Where() = %v", w)
	}
}

func TestLogsBuildUsesCoreColumns(t *testing.T) {
	sql, _, err := LogsBuild(context.Background(), SearchLogsRequest{
		Stream:  "default",
		Service: "api",
		Level:   "error",
		Limit:   5,
		Range:   TimeRange{Start: time.UnixMicro(100), End: time.UnixMicro(200)},
	}, FixedColumns("timestamp", "level", "service", "message"))
	if err != nil {
		t.Fatalf("LogsBuild: %v", err)
	}
	for _, want := range []string{
		"SELECT timestamp, level, service, message FROM default",
		"service = 'api'",
		"level = 'ERROR'",
		"timestamp >= 100",
		"timestamp <= 200",
		"LIMIT 5",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q: %q", want, sql)
		}
	}
}

func TestLogsBuildAddsOptionalColumnsFromResolver(t *testing.T) {
	sql, _, err := LogsBuild(context.Background(), SearchLogsRequest{
		Stream: "default",
		Limit:  10,
		Range:  TimeRange{Start: time.UnixMicro(100), End: time.UnixMicro(200)},
	}, FixedColumns("timestamp", "level", "service", "message", "status", "duration_ms"))
	if err != nil {
		t.Fatalf("LogsBuild: %v", err)
	}
	for _, want := range []string{"status", "duration_ms"} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing optional column %q: %q", want, sql)
		}
	}
}

func TestLogsBuildFallsBackOnResolverError(t *testing.T) {
	bad := failingResolver{}
	sql, _, err := LogsBuild(context.Background(), SearchLogsRequest{
		Stream: "default",
		Limit:  1,
		Range:  TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	}, bad)
	if err != nil {
		t.Fatalf("LogsBuild should not propagate resolver errors: %v", err)
	}
	if !strings.Contains(sql, "SELECT timestamp FROM default") {
		t.Errorf("fallback SQL unexpected: %q", sql)
	}
}

func TestAggregateLogsBuildRequiresGroupBy(t *testing.T) {
	if _, _, err := AggregateLogsBuild(AggregateLogsRequest{Stream: "default"}); err == nil {
		t.Fatal("expected error when GroupBy is empty")
	}
}

func TestAggregateLogsBuild(t *testing.T) {
	sql, _, err := AggregateLogsBuild(AggregateLogsRequest{
		Stream:  "default",
		GroupBy: "service",
		Where:   []string{"level = 'ERROR'"},
		Range:   TimeRange{Start: time.UnixMicro(100), End: time.UnixMicro(200)},
	})
	if err != nil {
		t.Fatalf("AggregateLogsBuild: %v", err)
	}
	want := "SELECT service AS g, count(*) AS c FROM default WHERE timestamp >= 100 AND timestamp <= 200 AND level = 'ERROR' GROUP BY service"
	if sql != want {
		t.Errorf("got %q, want %q", sql, want)
	}
}

func TestTracesBuild(t *testing.T) {
	sql, _, err := TracesBuild(context.Background(), SearchTracesRequest{
		Stream:  "traces",
		Service: "api",
		Limit:   3,
		Range:   TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	})
	if err != nil {
		t.Fatalf("TracesBuild: %v", err)
	}
	if !strings.Contains(sql, "FROM traces WHERE timestamp >= 1 AND timestamp <= 2 AND service = 'api'") {
		t.Errorf("SQL unexpected: %q", sql)
	}
	if !strings.HasSuffix(sql, "LIMIT 3") {
		t.Errorf("SQL missing LIMIT 3: %q", sql)
	}
}

func TestMetricsBuildDefaultsAndPercentile(t *testing.T) {
	sql, _, err := MetricsBuild(context.Background(), QueryMetricsRequest{
		Stream:      "metrics",
		MetricName:  "http_requests_total",
		Aggregation: "p95",
		Limit:       7,
		Range:       TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	})
	if err != nil {
		t.Fatalf("MetricsBuild: %v", err)
	}
	for _, want := range []string{
		"metric_name = 'http_requests_total'",
		"approx_percentile(value, 95) AS value",
		"LIMIT 7",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q: %q", want, sql)
		}
	}
}

type failingResolver struct{}

func (failingResolver) ResolveColumns(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("simulated resolver failure")
}
