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
	if len(w) != 2 || w[0] != "_timestamp >= 1000000" || w[1] != "_timestamp <= 2000000" {
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
	}, FixedColumns("_timestamp", "level", "service", "message"))
	if err != nil {
		t.Fatalf("LogsBuild: %v", err)
	}
	for _, want := range []string{
		`SELECT _timestamp, level, service, message FROM "default"`,
		"service = 'api'",
		"level = 'ERROR'",
		"_timestamp >= 100",
		"_timestamp <= 200",
		"ORDER BY _timestamp DESC LIMIT 5",
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
	}, FixedColumns("_timestamp", "level", "service", "message", "status", "duration_ms"))
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
	if !strings.Contains(sql, `SELECT _timestamp, level, service, message FROM "default"`) {
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
	want := `SELECT service AS g, count(*) AS c FROM "default" WHERE _timestamp >= 100 AND _timestamp <= 200 AND level = 'ERROR' GROUP BY service ORDER BY c DESC LIMIT 1000`
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
	if !strings.Contains(sql, `FROM "traces" WHERE _timestamp >= 1 AND _timestamp <= 2 AND service = 'api'`) {
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
		"approx_percentile_cont(value, 0.95) AS value",
		"LIMIT 7",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q: %q", want, sql)
		}
	}
}

func TestMetricsBuildAllAggregations(t *testing.T) {
	cases := []struct {
		agg   string
		match string
	}{
		{"avg", "avg(value) AS value"},
		{"sum", "sum(value) AS value"},
		{"count", "count(value) AS value"},
		{"max", "max(value) AS value"},
		{"min", "min(value) AS value"},
		{"p50", "approx_percentile_cont(value, 0.5) AS value"},
		{"p95", "approx_percentile_cont(value, 0.95) AS value"},
		{"p99", "approx_percentile_cont(value, 0.99) AS value"},
	}
	for _, tc := range cases {
		t.Run(tc.agg, func(t *testing.T) {
			sql, _, err := MetricsBuild(context.Background(), QueryMetricsRequest{
				Stream:      "metrics",
				MetricName:  "m",
				Aggregation: tc.agg,
				Range:       TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
			})
			if err != nil {
				t.Fatalf("MetricsBuild(%s): %v", tc.agg, err)
			}
			if !strings.Contains(sql, tc.match) {
				t.Errorf("agg=%s SQL=%q missing %q", tc.agg, sql, tc.match)
			}
		})
	}
}

func TestLogsBuildOmitsOptionalColumnNotInSchema(t *testing.T) {
	sql, _, err := LogsBuild(context.Background(), SearchLogsRequest{
		Stream: "default",
		Limit:  1,
		Range:  TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	}, FixedColumns("_timestamp", "level", "service", "message"))
	if err != nil {
		t.Fatalf("LogsBuild: %v", err)
	}
	for _, banned := range []string{"status", "duration_ms", "host"} {
		if strings.Contains(sql, banned) {
			t.Errorf("optional column %q leaked into SQL: %q", banned, sql)
		}
	}
}

func TestEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"o'reilly", "o''reilly"},
		{"a'b'c", "a''b''c"},
		{"back\\slash", "back\\slash"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escape(tc.in); got != tc.want {
			t.Errorf("escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTracesBuildStatusCasing(t *testing.T) {
	sql, _, err := TracesBuild(context.Background(), SearchTracesRequest{
		Stream: "traces",
		Status: "ERROR",
		Limit:  1,
		Range:  TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	})
	if err != nil {
		t.Fatalf("TracesBuild: %v", err)
	}
	if !strings.Contains(sql, "status = 'error'") {
		t.Errorf("status casing wrong: %q", sql)
	}
}

func TestTracesBuildMinDuration(t *testing.T) {
	sql, _, err := TracesBuild(context.Background(), SearchTracesRequest{
		Stream:     "traces",
		MinSpanDur: 5000,
		Limit:      1,
		Range:      TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	})
	if err != nil {
		t.Fatalf("TracesBuild: %v", err)
	}
	if !strings.Contains(sql, "duration >= 5000") {
		t.Errorf("min duration filter missing: %q", sql)
	}
}

func TestBuildersRejectUnsafeIdentifiers(t *testing.T) {
	rng := TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)}
	cases := map[string]func() error{
		"logs stream": func() error {
			_, _, err := LogsBuild(context.Background(), SearchLogsRequest{Stream: "x; DROP", Range: rng}, nil)
			return err
		},
		"metrics group_by": func() error {
			_, _, err := MetricsBuild(context.Background(), QueryMetricsRequest{Stream: "metrics", MetricName: "m", GroupBy: []string{"service) FROM x --"}, Range: rng})
			return err
		},
		"metrics aggregation": func() error {
			_, _, err := MetricsBuild(context.Background(), QueryMetricsRequest{Stream: "metrics", MetricName: "m", Aggregation: "sum(value)) --", Range: rng})
			return err
		},
		"aggregate group_by": func() error {
			_, _, err := AggregateLogsBuild(AggregateLogsRequest{Stream: "default", GroupBy: "a,b", Range: rng})
			return err
		},
		"traces stream": func() error {
			_, _, err := TracesBuild(context.Background(), SearchTracesRequest{Stream: "t r", Range: rng})
			return err
		},
		"logs level": func() error {
			_, _, err := LogsBuild(context.Background(), SearchLogsRequest{Stream: "default", Level: "x' OR '1'='1", Range: rng}, nil)
			return err
		},
	}
	for name, fn := range cases {
		if fn() == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestMetricsBuildGroupBy(t *testing.T) {
	sql, _, err := MetricsBuild(context.Background(), QueryMetricsRequest{
		Stream: "metrics", MetricName: "cpu_usage", Aggregation: "max",
		GroupBy: []string{"service", "host"},
		Range:   TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	})
	if err != nil {
		t.Fatalf("MetricsBuild: %v", err)
	}
	want := `SELECT service, host, max(value) AS value FROM "metrics" WHERE metric_name = 'cpu_usage' AND _timestamp >= 1 AND _timestamp <= 2 GROUP BY service, host ORDER BY value DESC LIMIT 100`
	if sql != want {
		t.Errorf("got  %q\nwant %q", sql, want)
	}
}

func TestLogsBuildOrderByDuration(t *testing.T) {
	sql, _, err := LogsBuild(context.Background(), SearchLogsRequest{
		Stream: "default", OrderBy: OrderByDuration, MinDurationMS: 1000,
		Range: TimeRange{Start: time.UnixMicro(1), End: time.UnixMicro(2)},
	}, nil)
	if err != nil {
		t.Fatalf("LogsBuild: %v", err)
	}
	if !strings.Contains(sql, "duration_ms >= 1000 ORDER BY duration_ms DESC") {
		t.Errorf("slow-request ordering missing: %q", sql)
	}
}

func TestLimitsAreClamped(t *testing.T) {
	sql, _, err := TracesBuild(context.Background(), SearchTracesRequest{Stream: "traces", Limit: 50000})
	if err != nil {
		t.Fatalf("TracesBuild: %v", err)
	}
	if !strings.HasSuffix(sql, "LIMIT 1000") {
		t.Errorf("limit not clamped: %q", sql)
	}
}

func TestTracesBuildByTraceIDIsChronological(t *testing.T) {
	sql, _, err := TracesBuild(context.Background(), SearchTracesRequest{Stream: "traces", TraceID: "abc"})
	if err != nil {
		t.Fatalf("TracesBuild: %v", err)
	}
	if !strings.Contains(sql, "ORDER BY _timestamp ASC") {
		t.Errorf("trace lookup should be ascending: %q", sql)
	}
}

type failingResolver struct{}

func (failingResolver) ResolveColumns(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("simulated resolver failure")
}
