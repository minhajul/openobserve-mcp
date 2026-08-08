// Command agent is a small LLM-based REPL that connects to the MCP
// server, picks MCP tools in response to natural-language questions,
// and prints a final natural-language answer.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=...
//	export MCP_SERVER_BIN=$(which mcp-server)   # or set in .env
//	agent -q "How many HTTP 500 errors in the last hour?"
//
// Pass `-i` for interactive REPL mode.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/puku/openobserve-mcp/internal/agent"
	"github.com/puku/openobserve-mcp/internal/config"
)

func main() {
	question := flag.String("q", "", "Single natural-language question. If empty, the agent reads from stdin.")
	interactive := flag.Bool("i", false, "Run as an interactive REPL.")
	mcpBin := flag.String("mcp-bin", os.Getenv("MCP_SERVER_BIN"), "Path to the mcp-server binary.")
	flag.Parse()

	if err := run(*question, *interactive, *mcpBin); err != nil {
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		os.Exit(1)
	}
}

func run(question string, interactive bool, mcpBin string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	a := agent.New(cfg, agent.Options{MCPBin: mcpBin})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if interactive {
		return runREPL(ctx, a)
	}
	if question == "" {
		// Read a single question from stdin.
		b, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err.Error() != "EOF" {
			return err
		}
		question = strings.TrimSpace(b)
		if question == "" {
			return fmt.Errorf("no question provided")
		}
	}
	return a.Run(ctx, question)
}

func runREPL(ctx context.Context, a *agent.Agent) error {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("openobserve-mcp agent (Ctrl-D to exit)")
	for {
		fmt.Print("> ")
		line, err := in.ReadString('\n')
		if err != nil {
			return nil
		}
		q := strings.TrimSpace(line)
		if q == "" {
			continue
		}
		if err := a.Run(ctx, q); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		fmt.Println()
	}
}
