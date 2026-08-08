package openobserve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/puku/openobserve-mcp/internal/config"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	return NewClient(cfg), srv
}

func TestEscape(t *testing.T) {
	if got := escape("O'Brien"); got != `O\'Brien` {
		t.Errorf("escape single quote = %q", got)
	}
	if got := escape(`a\b`); got != `a\\b` {
		t.Errorf("escape backslash = %q", got)
	}
}

func TestSearchLogsBuildsSQL(t *testing.T) {
	var capturedPath string
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"foo":"bar"}],"total":1,"took_ms":1}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	client := NewClient(cfg)

	resp, err := client.SearchLogs(context.Background(), SearchLogsRequest{
		Stream:          "default",
		Service:         "payment",
		Level:           "ERROR",
		MinDurationMS:   100,
		MessageContains: "O'Brien",
		StartTime:       time.Unix(0, 0),
		EndTime:         time.Unix(1, 0),
		Limit:           50,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if !strings.HasPrefix(capturedPath, "/api/default/_search") {
		t.Errorf("path = %q", capturedPath)
	}

	// capturedBody is JSON; the inner SQL field is JSON-escaped. Re-decode
	// it before substring matching.
	var req map[string]map[string]any
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	sql, _ := req["query"]["sql"].(string)
	if sql == "" {
		t.Fatalf("missing sql in body: %s", capturedBody)
	}
	for _, want := range []string{
		"service = 'payment'",
		"level = 'ERROR'",
		"duration_ms >= 100",
		"message ILIKE",
		`O\'Brien`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in SQL: %s", want, sql)
		}
	}
	if resp.QuerySQL == "" {
		t.Errorf("expected QuerySQL to be populated")
	}
	if len(resp.Hits) != 1 {
		t.Errorf("expected 1 hit, got %d", len(resp.Hits))
	}
}

func TestSearchLogsDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"hits":[],"total":0,"took_ms":1}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	client := NewClient(cfg)

	resp, err := client.SearchLogs(context.Background(), SearchLogsRequest{})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if !strings.Contains(resp.QuerySQL, "FROM default") {
		t.Errorf("default stream missing: %s", resp.QuerySQL)
	}
}

func TestHealthy(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(200)
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	if err := c.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	_, err := c.SearchLogs(context.Background(), SearchLogsRequest{Stream: "default"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v", err)
	}
}

func TestQueryMetricsBuildsSQL(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		captured = string(buf)
		_, _ = w.Write([]byte(`{"hits":[],"total":0}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	resp, err := c.QueryMetrics(context.Background(), QueryMetricsRequest{
		MetricName:  "http_requests_total",
		Aggregation: "p95",
		GroupBy:     []string{"service", "status"},
		StartTime:   time.Unix(0, 0),
		EndTime:     time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if !strings.Contains(resp.SQL, "approx_percentile(value, 95)") {
		t.Errorf("expected approx_percentile SQL, got: %s", resp.SQL)
	}
	if !strings.Contains(resp.SQL, "GROUP BY service, status") {
		t.Errorf("expected GROUP BY clause, got: %s", resp.SQL)
	}
	if !strings.Contains(captured, "metric_name = 'http_requests_total'") {
		t.Errorf("missing metric_name filter: %s", captured)
	}
}

func TestSearchTracesBuildsSQL(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		captured = string(buf)
		_, _ = w.Write([]byte(`{"hits":[],"total":0}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	resp, err := c.SearchTraces(context.Background(), SearchTracesRequest{
		Service:    "payment",
		Status:     "error",
		MinSpanDur: 500 * 1000,
		StartTime:  time.Unix(0, 0),
		EndTime:    time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if !strings.Contains(resp.SQL, "status = 'error'") {
		t.Errorf("expected status filter: %s", resp.SQL)
	}
	if !strings.Contains(resp.SQL, "duration >= 500000") {
		t.Errorf("expected duration filter: %s", resp.SQL)
	}
	if !strings.Contains(captured, "trace_id, span_id") {
		t.Errorf("expected span fields: %s", captured)
	}
}

func TestIngestLogs(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		got = string(buf)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	err := c.IngestLogs(context.Background(), "default", []LogEntry{
		{"timestamp": time.Now().UnixMicro(), "level": "INFO", "message": "hello"},
	})
	if err != nil {
		t.Fatalf("IngestLogs: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "[") {
		t.Errorf("expected top-level JSON array, got: %s", got)
	}
}
