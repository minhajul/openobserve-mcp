package openobserve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/puku/openobserve-mcp/internal/config"
)

type fakeTransport struct {
	queue   []*http.Response
	err     error
	gotReq  *http.Request
	gotBody []byte
	calls   int
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.gotBody = b
	}
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.queue) == 0 {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r, nil
}

func newTestClient(t *testing.T, ft *fakeTransport) *Client {
	t.Helper()
	cfg := &config.Config{
		OpenObserveURL:      "http://test:5080",
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	return NewClientWithTransport(cfg, ft)
}

func jsonResp(t *testing.T, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(append([]byte{}, b...))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func schemaResp(t *testing.T, streamName string) *http.Response {
	t.Helper()
	return jsonResp(t, map[string]any{
		"list": []map[string]any{
			{
				"name": streamName,
				"schema": []map[string]any{
					{"name": "timestamp"},
					{"name": "level"},
					{"name": "service"},
					{"name": "message"},
				},
			},
		},
	})
}

func TestResolveStreamDefaults(t *testing.T) {
	c := &Client{}
	cases := []struct {
		kind  StreamKind
		given string
		want  string
	}{
		{KindLog, "", "default"},
		{KindMetric, "", "metrics"},
		{KindTrace, "", "traces"},
		{KindLog, "custom", "custom"},
		{KindMetric, "custom_metrics", "custom_metrics"},
	}
	for _, tc := range cases {
		got := c.resolveStream(tc.kind, tc.given)
		if got != tc.want {
			t.Errorf("resolveStream(%d, %q) = %q, want %q", tc.kind, tc.given, got, tc.want)
		}
	}
}

func TestSearchLogsDefaultsStreamAndRange(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		schemaResp(t, "default"),
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if ft.gotReq == nil {
		t.Fatal("transport not called")
	}
	if !strings.HasSuffix(ft.gotReq.URL.Path, "/api/default/_search") {
		t.Errorf("URL path = %q, want suffix /api/default/_search", ft.gotReq.URL.Path)
	}
	var body struct {
		Query struct {
			SQL string `json:"sql"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(body.Query.SQL, "SELECT timestamp, level, service, message FROM default WHERE ") {
		t.Errorf("SQL prefix unexpected: %q", body.Query.SQL)
	}
	if !strings.Contains(body.Query.SQL, "timestamp >= ") {
		t.Errorf("SQL missing timestamp >= clause: %q", body.Query.SQL)
	}
}

func TestSearchLogsSchemaDrivenColumns(t *testing.T) {
	streamsResp := jsonResp(t, map[string]any{
		"list": []map[string]any{
			{
				"name": "default",
				"schema": []map[string]any{
					{"name": "timestamp"},
					{"name": "level"},
					{"name": "service"},
					{"name": "message"},
					{"name": "status"},
					{"name": "duration_ms"},
				},
			},
		},
	})
	empty := jsonResp(t, map[string]any{"hits": []any{}})
	empty2 := jsonResp(t, map[string]any{"hits": []any{}})
	ft := &fakeTransport{queue: []*http.Response{streamsResp, empty, empty2}}

	c := newTestClient(t, ft)
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("SearchLogs call 1 (streams): %v", err)
	}
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{Service: "api"}); err != nil {
		t.Fatalf("SearchLogs call 2 (search): %v", err)
	}
	var body struct {
		Query struct {
			SQL string `json:"sql"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, want := range []string{"status", "duration_ms"} {
		if !strings.Contains(body.Query.SQL, want) {
			t.Errorf("SQL missing %q: %q", want, body.Query.SQL)
		}
	}
}

func TestAggregateLogsDefaultsGroupByRequired(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	if _, err := c.AggregateLogs(context.Background(), AggregateLogsRequest{}); err == nil {
		t.Fatal("expected error when GroupBy is empty")
	}
}

func TestHealthy(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))},
	}}
	c := newTestClient(t, ft)
	if err := c.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
	if !strings.HasSuffix(ft.gotReq.URL.Path, "/health") {
		t.Errorf("Healthy path = %q, want suffix /health", ft.gotReq.URL.Path)
	}
}

func TestNewClientDefaultsTransport(t *testing.T) {
	cfg := &config.Config{
		OpenObserveURL:      "http://test:5080",
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	if c.http == nil {
		t.Fatal("NewClient must initialize a non-nil Transport")
	}
}

func TestSearchLogsSetsRequiredHeaders(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		schemaResp(t, "default"),
		jsonResp(t, map[string]any{"hits": []any{}, "total": 7, "took_ms": 3}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchLogs(context.Background(), SearchLogsRequest{Stream: "default"})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if resp.Total != 7 {
		t.Errorf("resp.Total = %d, want 7", resp.Total)
	}
	if resp.QuerySQL == "" {
		t.Error("QuerySQL should be populated")
	}
	if got := ft.gotReq.Header.Get("Authorization"); got == "" {
		t.Error("Authorization header missing")
	}
	if got := ft.gotReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := ft.gotReq.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
}

func TestAggregateLogsRoundTrip(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{
			"hits": []map[string]any{
				{"g": "api", "c": 12},
				{"g": "checkout", "c": 3},
			},
			"total":     15,
			"query_sql": "SELECT service, count(*) ...",
		}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.AggregateLogs(context.Background(), AggregateLogsRequest{
		Stream: "default", GroupBy: "service", Where: []string{"level = 'ERROR'"},
		Range: TimeRange{Start: time.Now().Add(-time.Hour), End: time.Now()},
	})
	if err != nil {
		t.Fatalf("AggregateLogs: %v", err)
	}
	if resp.Total != 15 {
		t.Errorf("Total = %d, want 15", resp.Total)
	}
	if got := resp.Groups["api"]; got != 12 {
		t.Errorf(`Groups["api"] = %d, want 12`, got)
	}
	if got := resp.Groups["checkout"]; got != 3 {
		t.Errorf(`Groups["checkout"] = %d, want 3`, got)
	}
}

func TestAggregateLogsDecodeRequiresTypedHitShape(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{
			"hits": []map[string]any{{"g": "x", "c": "not-a-number"}},
		}),
	}}
	c := newTestClient(t, ft)
	_, err := c.AggregateLogs(context.Background(), AggregateLogsRequest{
		Stream: "default", GroupBy: "service",
		Range: TimeRange{Start: time.Now().Add(-time.Hour), End: time.Now()},
	})
	if err == nil {
		t.Fatal("expected error when count field is malformed")
	}
	if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected decode error, got %q", err.Error())
	}
}

func TestSearchTracesRoundTrip(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{
			"hits":    []map[string]any{{"trace_id": "abc", "service": "api"}},
			"total":   1,
			"took_ms": 2,
		}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchTraces(context.Background(), SearchTracesRequest{
		Stream: "traces", Service: "api", Limit: 5,
		Range: TimeRange{Start: time.Now().Add(-time.Hour), End: time.Now()},
	})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
	if len(resp.Hits) != 1 {
		t.Errorf("Hits length = %d, want 1", len(resp.Hits))
	}
}

func TestQueryMetricsRoundTrip(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{
			"hits":  []map[string]any{{"value": 42.5}},
			"total": 1,
		}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.QueryMetrics(context.Background(), QueryMetricsRequest{
		Stream: "metrics", MetricName: "http_requests_total", Aggregation: "avg", Limit: 10,
		Range: TimeRange{Start: time.Now().Add(-time.Hour), End: time.Now()},
	})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if resp.QuerySQL == "" {
		t.Error("QuerySQL should be populated")
	}
	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
}

func TestHealthyNon2xxReturnsError(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unhealthy"))},
	}}
	c := newTestClient(t, ft)
	if err := c.Healthy(context.Background()); err == nil {
		t.Fatal("expected error for non-2xx health response")
	}
}

func TestStreamSchemaCacheHitAvoidsSecondCall(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{
			"list": []map[string]any{
				{"name": "default", "schema": []map[string]any{{"name": "timestamp"}}},
			},
		}),
		jsonResp(t, map[string]any{"hits": []any{}}),
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("first SearchLogs: %v", err)
	}
	firstCalls := ft.calls
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("second SearchLogs: %v", err)
	}
	if ft.calls-firstCalls != 1 {
		t.Errorf("second call should reuse cached schema (1 transport call), got %d extra", ft.calls-firstCalls)
	}
}

func TestStreamSchemaNotFoundFallsBackToTimestamp(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{"list": []map[string]any{}}),
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchLogs(context.Background(), SearchLogsRequest{Stream: "missing"})
	if err != nil {
		t.Fatalf("SearchLogs with missing stream should fall back to timestamp-only query, got error: %v", err)
	}
	if !strings.Contains(resp.QuerySQL, "SELECT timestamp FROM missing") {
		t.Errorf("expected fallback SQL, got %q", resp.QuerySQL)
	}
}
