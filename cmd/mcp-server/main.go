// Command mcp-server starts the OpenObserve MCP server over stdio.
//
// It reads configuration from environment variables and exposes
// observability capabilities as MCP tools. Any MCP-compatible client
// launches this binary as a subprocess.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/puku/openobserve-mcp/internal/config"
	"github.com/puku/openobserve-mcp/internal/mcp"
	"github.com/puku/openobserve-mcp/internal/openobserve"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.MCPLogFile, cfg.MCPLogLevel)
	logger.Info("starting mcp-server",
		slog.String("openobserve_url", cfg.OpenObserveURL),
		slog.String("openobserve_org", cfg.OpenObserveOrg),
	)

	client := openobserve.NewClient(cfg)
	if err := client.Healthy(context.Background()); err != nil {
		logger.Warn("openobserve health check failed (continuing)", slog.String("error", err.Error()))
	}

	srv := mcp.New(client, mcp.Options{Logger: logger})
	logger.Info("mcp-server ready, serving over stdio")
	if err := srv.ServeStdio(); err != nil {
		return fmt.Errorf("stdio server: %w", err)
	}
	return nil
}

func newLogger(path, levelName string) *slog.Logger {
	level := slog.LevelInfo
	if levelName == "debug" {
		level = slog.LevelDebug
	}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log file %s: %v\n", path, err)
			return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
		}
		return slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
