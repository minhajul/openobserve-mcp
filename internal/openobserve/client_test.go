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

func schemaResp(t *testing.T, cols ...string) *http.Response {
	t.Helper()
	if len(cols) == 0 {
		cols = []string{"_timestamp", "level", "service", "message"}
	}
	fields := make([]map[string]any, len(cols))
	for i, c := range cols {
		fields[i] = map[string]any{"name": c}
	}
	return jsonResp(t, map[string]any{"schema": fields})
}

func lastSQL(t *testing.T, ft *fakeTransport) string {
	t.Helper()
	var body struct {
		Query struct {
			SQL string `json:"sql"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return body.Query.SQL
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
		schemaResp(t),
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
	if !strings.HasPrefix(body.Query.SQL, `SELECT _timestamp, level, service, message FROM "default" WHERE `) {
		t.Errorf("SQL prefix unexpected: %q", body.Query.SQL)
	}
	if !strings.Contains(body.Query.SQL, "_timestamp >= ") {
		t.Errorf("SQL missing timestamp >= clause: %q", body.Query.SQL)
	}
}

func TestSearchLogsSchemaDrivenColumns(t *testing.T) {
	streamsResp := schemaResp(t, "_timestamp", "level", "service", "message", "status", "duration_ms")
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
	if !strings.HasSuffix(ft.gotReq.URL.Path, "/healthz") {
		t.Errorf("Healthy path = %q, want suffix /healthz", ft.gotReq.URL.Path)
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
		schemaResp(t),
		jsonResp(t, map[string]any{"hits": []any{map[string]any{"level": "INFO"}}, "total": 1, "took": 3}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchLogs(context.Background(), SearchLogsRequest{Stream: "default"})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if resp.Count != 1 || resp.TookMs != 3 {
		t.Errorf("Count, TookMs = %d, %d; want 1, 3", resp.Count, resp.TookMs)
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
	want := []AggregateGroup{{Key: "api", Count: 12}, {Key: "checkout", Count: 3}}
	if len(resp.Groups) != 2 || resp.Groups[0] != want[0] || resp.Groups[1] != want[1] {
		t.Errorf("Groups = %v, want %v", resp.Groups, want)
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
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
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
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
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
		schemaResp(t),
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

func TestStreamSchemaNotFoundFallsBackToCoreColumns(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"code":404}`))},
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchLogs(context.Background(), SearchLogsRequest{Stream: "missing"})
	if err != nil {
		t.Fatalf("SearchLogs with missing schema should degrade, got error: %v", err)
	}
	if !strings.Contains(resp.QuerySQL, `SELECT _timestamp, level, service, message FROM "missing"`) {
		t.Errorf("expected core-column fallback SQL, got %q", resp.QuerySQL)
	}
}

func TestSchemaLookupUsesPerStreamEndpoint(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{schemaResp(t)}}
	c := newTestClient(t, ft)
	if _, err := c.ResolveColumns(context.Background(), "default"); err != nil {
		t.Fatalf("ResolveColumns: %v", err)
	}
	if got := ft.gotReq.URL.Path; got != "/api/default/streams/default/schema" {
		t.Errorf("schema path = %q", got)
	}
}

func TestSearchHumanizesTimestamps(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{"hits": []map[string]any{{"_timestamp": 1_700_000_000_000_000, "trace_id": "abc"}}}),
	}}
	c := newTestClient(t, ft)
	resp, err := c.SearchTraces(context.Background(), SearchTracesRequest{TraceID: "abc"})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	h := resp.Hits[0]
	if _, ok := h["_timestamp"]; ok {
		t.Error("_timestamp should be removed from hits")
	}
	if got := h["timestamp"]; got != "2023-11-14T22:13:20Z" {
		t.Errorf("timestamp = %v, want RFC3339", got)
	}
}

func TestSearchBodyUsesNormalizedRange(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{jsonResp(t, map[string]any{"hits": []any{}})}}
	c := newTestClient(t, ft)
	if _, err := c.GetTrace(context.Background(), "traces", "abc", TimeRange{}); err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	var body struct {
		Query struct {
			StartTime int64 `json:"start_time"`
			EndTime   int64 `json:"end_time"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Query.StartTime <= 0 || body.Query.EndTime-body.Query.StartTime != traceLookupWindow.Microseconds() {
		t.Errorf("upstream window = [%d, %d], want a 7d window ending now", body.Query.StartTime, body.Query.EndTime)
	}
	if sql := lastSQL(t, ft); !strings.Contains(sql, "trace_id = 'abc'") {
		t.Errorf("SQL = %q", sql)
	}
}

func TestQuerySQLNotSerialized(t *testing.T) {
	b, err := json.Marshal(&SearchLogsResponse{QuerySQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SELECT") {
		t.Errorf("SQL leaked into client JSON: %s", b)
	}
}
