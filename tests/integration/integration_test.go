//go:build integration

// Package integration contains end-to-end tests that exercise the MCP
// server against a real OpenObserve instance.
//
// To run:
//
//	docker compose up -d
//	go run ./cmd/seed
//	go test -tags=integration ./tests/integration/...
//
// OPENOBSERVE_* env vars are honored.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/puku/openobserve-mcp/internal/config"
	"github.com/puku/openobserve-mcp/internal/openobserve"
)

func mustLoad(t *testing.T) *config.Config {
	t.Helper()
	for _, k := range []string{
		"OPENOBSERVE_URL", "OPENOBSERVE_ORG", "OPENOBSERVE_USERNAME", "OPENOBSERVE_PASSWORD",
	} {
		if os.Getenv(k) == "" {
			t.Skipf("integration test requires %s", k)
		}
	}
	c, err := config.LoadOpenObserveOnly()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return c
}

// TestOpenObserveHealthAndIngest is the basic sanity test: container
// reachable, ingestion succeeds, search returns the inserted rows.
func TestOpenObserveHealthAndIngest(t *testing.T) {
	cfg := mustLoad(t)
	client := openobserve.NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Healthy(ctx); err != nil {
		t.Skipf("openobserve not healthy: %v", err)
	}

	now := time.Now().UTC()
	strm := fmt.Sprintf("it_%d", now.Unix())
	entry := openobserve.LogEntry{
		"timestamp":   now.UnixMicro(),
		"level":       "ERROR",
		"service":     "integration-test",
		"environment": "ci",
		"message":     "hello-from-integration-test",
	}
	if err := client.IngestLogs(ctx, strm, []openobserve.LogEntry{entry}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Allow OpenObserve to flush.
	time.Sleep(2 * time.Second)

	resp, err := client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:    strm,
		Limit:     10,
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("expected at least one hit, got 0")
	}
}

// TestMCPServerEndToEnd starts the MCP server as a subprocess and
// exercises a real tool call through the stdio transport.
func TestMCPServerEndToEnd(t *testing.T) {
	cfg := mustLoad(t)
	bin, err := buildMCPServer(t)
	if err != nil {
		t.Fatalf("build mcp server: %v", err)
	}
	env := []string{
		"OPENOBSERVE_URL=" + cfg.OpenObserveURL,
		"OPENOBSERVE_ORG=" + cfg.OpenObserveOrg,
		"OPENOBSERVE_USERNAME=" + cfg.OpenObserveUsername,
		"OPENOBSERVE_PASSWORD=" + cfg.OpenObservePassword,
		"OPENOBSERVE_TIMEOUT=30s",
	}

	client, err := mcpclient.NewStdioMCPClientWithOptions(
		bin, env, nil,
		transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = env
			return cmd, nil
		}),
	)
	if err != nil {
		t.Fatalf("start mcp client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "integration", Version: "0.0.0"},
		},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) < 5 {
		t.Errorf("expected at least 5 tools, got %d", len(tools.Tools))
	}

	cases := []struct {
		name     string
		toolName string
		args     map[string]any
	}{
		{
			name:     "search_logs_payment_errors",
			toolName: "search_logs",
			args:     map[string]any{"service": "payment", "level": "ERROR", "since": "24h", "limit": 5.0},
		},
		{
			name:     "search_errors_no_filter",
			toolName: "search_errors",
			args:     map[string]any{"since": "24h", "limit": 5.0},
		},
		{
			name:     "search_traces_failed",
			toolName: "search_traces",
			args:     map[string]any{"status": "error", "since": "24h", "limit": 5.0},
		},
		{
			name:     "get_recent_logs",
			toolName: "get_recent_logs",
			args:     map[string]any{"limit": 5.0},
		},
		{
			name:     "get_slow_requests",
			toolName: "get_slow_requests",
			args:     map[string]any{"min_duration_ms": 1500.0, "limit": 5.0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Name: tc.toolName, Arguments: tc.args},
			})
			if err != nil {
				t.Fatalf("call %s: %v", tc.toolName, err)
			}
			if res.IsError {
				t.Fatalf("tool error: %v", res.Content)
			}
			gotText := false
			for _, c := range res.Content {
				if tc, ok := mcp.AsTextContent(c); ok {
					gotText = true
					var parsed map[string]any
					if err := json.Unmarshal([]byte(tc.Text), &parsed); err != nil {
						t.Fatalf("decode: %v text=%q", err, tc.Text)
					}
				}
			}
			if !gotText {
				t.Fatalf("no text content in tool result")
			}
		})
	}
}

// TestMCPErrorHandling exercises the path where the LLM agent passes
// invalid arguments to the MCP server.
func TestMCPErrorHandling(t *testing.T) {
	bin, err := buildMCPServer(t)
	if err != nil {
		t.Fatalf("build mcp server: %v", err)
	}
	env := []string{
		"OPENOBSERVE_URL=http://127.0.0.1:1", // unreachable
		"OPENOBSERVE_ORG=default",
		"OPENOBSERVE_USERNAME=u",
		"OPENOBSERVE_PASSWORD=p",
		"OPENOBSERVE_TIMEOUT=2s",
	}
	client, err := mcpclient.NewStdioMCPClientWithOptions(
		bin, env, nil,
		transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = env
			return cmd, nil
		}),
	)
	if err != nil {
		t.Fatalf("start mcp client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "integration", Version: "0.0.0"},
		},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Call a tool that will hit the unreachable OpenObserve.
	res, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "search_logs",
			Arguments: map[string]any{"service": "x"},
		},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected MCP error result when OpenObserve is unreachable")
	}
}

// TestDirectClientIngestAndQuery validates the OpenObserve client
// abstraction (no MCP) by ingesting and re-querying.
func TestDirectClientIngestAndQuery(t *testing.T) {
	cfg := mustLoad(t)
	client := openobserve.NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a fixed stream per test run.
	strm := fmt.Sprintf("metrics_test_%d", time.Now().Unix())
	now := time.Now().UTC()
	metrics := []map[string]any{
		{
			"timestamp":   now.UnixMicro(),
			"metric_name": "http_requests_total",
			"service":     "integration",
			"value":       42.0,
		},
	}
	if err := client.IngestMetrics(ctx, strm, metrics); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	time.Sleep(2 * time.Second)

	resp, err := client.QueryMetrics(ctx, openobserve.QueryMetricsRequest{
		Stream:     strm,
		MetricName: "http_requests_total",
		StartTime:  now.Add(-1 * time.Hour),
		EndTime:    now.Add(1 * time.Hour),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("expected at least one metric sample, got 0")
	}
}

// buildMCPServer builds the mcp-server binary into a temp dir and
// returns its absolute path.
func buildMCPServer(t *testing.T) (string, error) {
	t.Helper()
	// Run `go build` against the project root, not the test directory.
	root, err := projectRoot()
	if err != nil {
		return "", err
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-server")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mcp-server")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return bin, nil
}

// projectRoot walks up from the test file's directory to the directory
// containing go.mod.
func projectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", cwd)
		}
		dir = parent
	}
	return "", fmt.Errorf("go.mod not found above %s", cwd)
}
