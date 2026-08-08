// Command verify exercises every documented natural-language query
// against the running MCP server. It does NOT require an LLM API key
// — the "tool selection" is hard-coded to mirror what a competent LLM
// would do for each prompt.
//
// Usage:
//
//	OPENOBSERVE_URL=http://localhost:5080 \
//	OPENOBSERVE_USERNAME=root@example.com \
//	OPENOBSERVE_PASSWORD=Complexpass#123 \
//	go run ./scripts/verify
//
// The script writes a transcript to stdout suitable for inclusion in a
// verification report.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/puku/openobserve-mcp/internal/config"
)

// nullWriter discards everything written to it.

type scenario struct {
	Question string         `json:"question"`
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadOpenObserveOnly()
	if err != nil {
		return err
	}

	bin := os.Getenv("MCP_SERVER_BIN")
	if bin == "" {
		bin = "./bin/mcp-server"
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("mcp-server binary not found at %s (run `make build`)", bin)
	}

	env := []string{
		"OPENOBSERVE_URL=" + cfg.OpenObserveURL,
		"OPENOBSERVE_ORG=" + cfg.OpenObserveOrg,
		"OPENOBSERVE_USERNAME=" + cfg.OpenObserveUsername,
		"OPENOBSERVE_PASSWORD=" + cfg.OpenObservePassword,
		"OPENOBSERVE_TIMEOUT=" + cfg.OpenObserveTimeout.String(),
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
		return fmt.Errorf("start mcp: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "verify", Version: "0.1.0"},
		},
	}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	toolsResp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Println("MCP server reports the following tools:")
	for _, t := range toolsResp.Tools {
		fmt.Printf("  - %s\n", t.Name)
	}
	fmt.Println()

	scenarios := []scenario{
		{
			Question: "Show me the latest errors from the payment service.",
			Tool:     "search_logs",
			Args:     map[string]any{"service": "payment", "level": "ERROR", "since": "24h", "limit": 3},
		},
		{
			Question: "How many HTTP 500 errors occurred in the last hour?",
			Tool:     "search_errors",
			Args:     map[string]any{"since": "24h", "limit": 3},
		},
		{
			Question: "Show me the slowest API requests.",
			Tool:     "get_slow_requests",
			Args:     map[string]any{"min_duration_ms": 1500, "limit": 3},
		},
		{
			Question: "Which service has the most errors?",
			Tool:     "get_service_errors",
			Args:     map[string]any{"since": "24h"},
		},
		{
			Question: "Find traces associated with failed payment requests.",
			Tool:     "search_traces",
			Args:     map[string]any{"service": "payment", "status": "error", "since": "24h", "limit": 3},
		},
		{
			Question: "Show me checkout requests that took more than 2 seconds.",
			Tool:     "search_logs",
			Args:     map[string]any{"service": "checkout", "min_duration_ms": 2000, "since": "24h", "limit": 3},
		},
		{
			Question: "Average http_request_duration_ms across all services.",
			Tool:     "query_metrics",
			Args:     map[string]any{"metric": "http_request_duration_ms", "aggregation": "avg", "since": "24h"},
		},
	}

	for _, s := range scenarios {
		fmt.Printf("Question : %s\n", s.Question)
		fmt.Printf("Tool     : %s\n", s.Tool)
		fmt.Printf("Args     : %s\n", compactJSON(s.Args))
		res, err := client.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: s.Tool, Arguments: s.Args},
		})
		if err != nil {
			fmt.Printf("ERROR    : %v\n\n", err)
			continue
		}
		if res.IsError {
			fmt.Printf("TOOL ERR : %s\n\n", firstText(res))
			continue
		}
		text := firstText(res)
		fmt.Printf("RESPONSE : %s\n\n", truncate(text, 600))
	}
	return nil
}

func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := mcp.AsTextContent(c); ok {
			return tc.Text
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func compactJSON(v any) string {
	b, _ := json.Marshal(v)
	return strings.ReplaceAll(string(b), ",", ", ")
}
