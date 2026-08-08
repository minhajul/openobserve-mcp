// Package mcp implements an MCP server that exposes OpenObserve
// observability capabilities as a set of typed tools.
//
// The tools hide OpenObserve's URL structure, stream names, and SQL
// dialect from the model.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/puku/openobserve-mcp/internal/openobserve"
)

// Server wraps an MCP server with an OpenObserve client and structured
// logger.
type Server struct {
	mcp    *server.MCPServer
	client *openobserve.Client
	logger *slog.Logger
}

// Options configure the MCP server.
type Options struct {
	Name    string
	Version string
	Logger  *slog.Logger
}

// New creates an MCP server with all observability tools registered.
func New(client *openobserve.Client, opts Options) *Server {
	if opts.Name == "" {
		opts.Name = "openobserve-mcp"
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	s := &Server{
		client: client,
		logger: opts.Logger,
	}

	mcpServer := server.NewMCPServer(
		opts.Name,
		opts.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	s.registerTools(mcpServer)
	s.mcp = mcpServer
	return s
}

// StdioServer returns a stdio transport that any MCP-compatible client
// can launch as a subprocess.
func (s *Server) StdioServer() *server.StdioServer {
	return server.NewStdioServer(s.mcp)
}

// ServeStdio blocks while serving MCP over stdio.
func (s *Server) ServeStdio() error {
	return s.StdioServer().Listen(context.Background(), os.Stdin, os.Stdout)
}

// ServeHTTP starts an HTTP MCP server. Used by integration tests.
func (s *Server) ServeHTTP(addr string) (*http.Server, error) {
	httpServer := server.NewStreamableHTTPServer(
		s.mcp,
		server.WithStateLess(true),
	)
	hs := &http.Server{
		Addr:              addr,
		Handler:           httpServer,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("mcp http server failed", slog.String("error", err.Error()))
		}
	}()
	return hs, nil
}

// MCPServer exposes the underlying MCP server (used by integration tests).
func (s *Server) MCPServer() *server.MCPServer { return s.mcp }

// logToolCall logs structured info about each tool invocation.
func (s *Server) logToolCall(tool string, start time.Time, err error, extra ...slog.Attr) {
	dur := time.Since(start)
	attrs := []slog.Attr{
		slog.String("component", "mcp"),
		slog.String("tool", tool),
		slog.Int64("duration_ms", dur.Milliseconds()),
	}
	attrs = append(attrs, extra...)
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
		s.logger.Error("tool invocation failed", attrsAsArgs(attrs)...)
		return
	}
	s.logger.Info("tool invocation", attrsAsArgs(attrs)...)
}

func attrsAsArgs(attrs []slog.Attr) []any {
	out := make([]any, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a)
	}
	return out
}

// jsonResult marshals `data` into a JSON-formatted tool result.
func jsonResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}

// errorResult converts an error into a structured tool result with
// isError=true. The error message is included verbatim.
func errorResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

// parseDuration parses a Go-style duration string ("15m", "1h"). It is
// tolerant of bare integer minutes ("15") for ergonomics.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Fallback: treat as minutes.
	var mins int
	if _, err := fmt.Sscanf(s, "%d", &mins); err == nil {
		return time.Duration(mins) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid duration %q", s)
}

// resolveTimeWindow turns "last 30m" style inputs into concrete bounds.
func resolveTimeWindow(start, end, since string) (time.Time, time.Time, error) {
	now := time.Now()
	if end != "" {
		t, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_time: %w", err)
		}
		return t.Add(-time.Hour), t, nil // if start omitted, default to 1h before end
	}
	if since != "" {
		d, err := parseDuration(since)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return now.Add(-d), now, nil
	}
	return now.Add(-1 * time.Hour), now, nil
}
