package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/puku/openobserve-mcp/internal/openobserve"
)

type Server struct {
	mcp    *server.MCPServer
	client *openobserve.Client
	logger *slog.Logger
}

type Options struct {
	Name    string
	Version string
	Logger  *slog.Logger
}

func New(client *openobserve.Client, opts Options) *Server {
	if opts.Name == "" {
		opts.Name = "openobserve-mcp"
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.Logger == nil {
		// stdout carries the MCP stdio protocol; logs must never go there.
		opts.Logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
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

func (s *Server) StdioServer() *server.StdioServer {
	return server.NewStdioServer(s.mcp)
}

func (s *Server) ServeStdio() error {
	return s.StdioServer().Listen(context.Background(), os.Stdin, os.Stdout)
}

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

func jsonResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}

func errorResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

// parseDuration accepts Go durations ("90m", "1h30m"), a day suffix ("7d"),
// or a bare integer meaning minutes ("30").
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else if n, err := strconv.Atoi(s); err == nil {
		d = time.Duration(n) * time.Minute
	} else if d, err = time.ParseDuration(s); err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 15m, 1h, 7d)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}

func resolveTimeWindow(start, end, since string) (time.Time, time.Time, error) {
	now := time.Now()
	endT := now
	if end != "" {
		t, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_time: %w", err)
		}
		endT = t
	}
	startT := endT.Add(-time.Hour)
	switch {
	case start != "":
		t, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_time: %w", err)
		}
		startT = t
	case since != "" && end == "":
		d, err := parseDuration(since)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if d > 0 {
			startT = endT.Add(-d)
		}
	}
	if !startT.Before(endT) {
		return time.Time{}, time.Time{}, fmt.Errorf("start_time must be before end_time")
	}
	return startT, endT, nil
}

// windowFromArgs resolves start_time/end_time/since, using defaultSince when
// none are given.
func windowFromArgs(args map[string]any, defaultSince string) (openobserve.TimeRange, error) {
	since := stringArg(args, "since", "")
	if since == "" {
		since = defaultSince
	}
	startT, endT, err := resolveTimeWindow(
		stringArg(args, "start_time", ""),
		stringArg(args, "end_time", ""),
		since,
	)
	if err != nil {
		return openobserve.TimeRange{}, err
	}
	return openobserve.TimeRange{Start: startT.UTC(), End: endT.UTC()}, nil
}

func stringArg(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func floatArg(args map[string]any, key string, def float64) float64 {
	if v, ok := args[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
