package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	var mins int
	if _, err := fmt.Sscanf(s, "%d", &mins); err == nil {
		return time.Duration(mins) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid duration %q", s)
}

func resolveTimeWindow(start, end, since string) (time.Time, time.Time, error) {
	now := time.Now()
	if end != "" {
		t, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_time: %w", err)
		}
		if start != "" {
			s, err := time.Parse(time.RFC3339, start)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("invalid start_time: %w", err)
			}
			return s, t, nil
		}
		return t.Add(-time.Hour), t, nil
	}
	if start != "" {
		s, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_time: %w", err)
		}
		return s, now, nil
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

func windowFromArgs(args map[string]any) (openobserve.TimeRange, error) {
	startT, endT, err := resolveTimeWindow(
		stringArg(args, "start_time", ""),
		stringArg(args, "end_time", ""),
		stringArg(args, "since", "1h"),
	)
	if err != nil {
		return openobserve.TimeRange{}, err
	}
	return openobserve.TimeRange{Start: startT, End: endT}, nil
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
