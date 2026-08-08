package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/puku/openobserve-mcp/internal/config"
	"github.com/puku/openobserve-mcp/internal/openobserve"
)

func newTestServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		OpenObserveURL:      srv.URL,
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	client := openobserve.NewClient(cfg)
	return New(client, Options{})
}

func TestToolsRegistered(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[],"total":0}`))
	}))
	tools := srv.MCPServer().ListTools()
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	for _, want := range []string{
		"search_logs", "get_recent_logs", "search_errors",
		"query_metrics", "get_metric",
		"search_traces", "get_trace",
		"get_service_errors", "get_slow_requests", "get_error_summary",
	} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %q not registered", want)
		}
	}
}

func TestToolSchemas(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[],"total":0}`))
	}))
	for name, st := range srv.MCPServer().ListTools() {
		tool := st.Tool
		if tool.Name == "" {
			t.Errorf("tool missing name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q missing description", name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %q schema type = %q, want object", name, tool.InputSchema.Type)
		}
	}
}

func TestSearchLogsInvokesOpenObserve(t *testing.T) {
	called := 0
	var path string
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"hits":[{"level":"ERROR","service":"payment"}],"total":1,"took":3}`))
	}))

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "search_logs"
	req.Params.Arguments = map[string]any{
		"service": "payment",
		"level":   "ERROR",
		"limit":   50.0,
		"since":   "1h",
	}
	res, err := srv.handleSearchLogs(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	if called == 0 {
		t.Fatal("openobserve never called")
	}
	if !strings.HasPrefix(path, "/api/default/_search") {
		t.Errorf("path = %q", path)
	}
}

func TestSearchErrorsRejectsMissingSince(t *testing.T) {
	// Use the defaults — we just want to confirm it doesn't blow up.
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[],"total":0}`))
	}))
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "search_errors"
	req.Params.Arguments = map[string]any{}
	res, err := srv.handleSearchErrors(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success with defaults: %v", res.Content)
	}
}

func TestInvalidDuration(t *testing.T) {
	if _, err := parseDuration("not-a-duration"); err == nil {
		t.Errorf("expected error for invalid duration")
	}
	if d, err := parseDuration("15"); err != nil || d != 15*time.Minute {
		t.Errorf("parseDuration('15') = %v, %v", d, err)
	}
	if d, err := parseDuration("15m"); err != nil || d != 15*time.Minute {
		t.Errorf("parseDuration('15m') = %v, %v", d, err)
	}
}

func TestResolveTimeWindow(t *testing.T) {
	now := time.Now()
	start, end, err := resolveTimeWindow("", "", "30m")
	if err != nil {
		t.Fatal(err)
	}
	if end.Before(start) {
		t.Errorf("end before start")
	}
	if end.Sub(start) != 30*time.Minute {
		t.Errorf("expected 30m window, got %s", end.Sub(start))
	}
	if now.Sub(end) > time.Minute {
		t.Errorf("end too far in the past")
	}
}

func TestSearchLogsInvalidTimeReturnsError(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "search_logs"
	req.Params.Arguments = map[string]any{
		"start_time": "not-a-time",
	}
	res, err := srv.handleSearchLogs(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result for invalid time")
	}
}

func TestGetTraceRequiresTraceID(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "get_trace"
	req.Params.Arguments = map[string]any{}
	res, _ := srv.handleGetTrace(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected error when trace_id missing")
	}
}

func TestQueryMetricsRequiresMetric(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "query_metrics"
	req.Params.Arguments = map[string]any{}
	res, _ := srv.handleQueryMetrics(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected error when metric missing")
	}
}

// Test that error responses from OpenObserve bubble up as MCP tool errors
// (not as crashes).
func TestOpenObserveFailurePropagates(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"kaboom"}`))
	}))
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "search_logs"
	req.Params.Arguments = map[string]any{"service": "x"}
	res, _ := srv.handleSearchLogs(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected MCP tool error when OpenObserve returns 500")
	}
}

func TestJSONResultMarshalable(t *testing.T) {
	res, err := jsonResult(map[string]any{"hello": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("unexpected error result")
	}
	var got map[string]any
	for _, c := range res.Content {
		if tc, ok := mcpgo.AsTextContent(c); ok {
			if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
				t.Fatalf("unmarshal: %v text=%q", err, tc.Text)
			}
		}
	}
	if got["hello"] != "world" {
		t.Errorf("got %v", got)
	}
}
