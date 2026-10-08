package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/puku/openobserve-mcp/internal/config"
	"github.com/puku/openobserve-mcp/internal/openobserve"
)

func main() {
	hostFlag := flag.String("host", "api", "Synthetic hostname used in seed records.")
	flag.Parse()

	if err := run(*hostFlag); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

func run(host string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("seeding openobserve",
		slog.String("url", cfg.OpenObserveURL),
		slog.String("org", cfg.OpenObserveOrg),
	)

	client := openobserve.NewClient(cfg)
	ctx := context.Background()

	if err := client.Healthy(ctx); err != nil {
		return fmt.Errorf("openobserve unhealthy: %w", err)
	}

	// Records are spread over the hour before now (offsets are deterministic),
	// so the tools' default 1h window sees the full data set right after seeding.
	// OpenObserve rejects event times older than ZO_INGEST_ALLOWED_UPTO (5h).
	seed := time.Now().UTC().Truncate(time.Second)
	logs := generateLogs(seed, host)
	metrics := generateMetrics(seed, host)
	spans := generateSpans(seed, host)

	if err := client.IngestLogs(ctx, "default", logs); err != nil {
		return fmt.Errorf("ingest logs: %w", err)
	}
	logger.Info("logs ingested", slog.Int("count", len(logs)))

	if err := client.IngestMetrics(ctx, "metrics", metrics); err != nil {
		return fmt.Errorf("ingest metrics: %w", err)
	}
	logger.Info("metrics ingested", slog.Int("count", len(metrics)))

	if err := client.IngestSpans(ctx, "traces", spans); err != nil {
		return fmt.Errorf("ingest spans: %w", err)
	}
	logger.Info("spans ingested", slog.Int("count", len(spans)))

	manifest := map[string]any{
		"seed_window_start": seed.Add(-1 * time.Hour).Format(time.RFC3339),
		"seed_window_end":   seed.Format(time.RFC3339),
		"counts": map[string]int{
			"logs":    len(logs),
			"metrics": len(metrics),
			"traces":  len(spans),
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(manifest)
	return nil
}

var services = []string{"api", "checkout", "payment", "auth", "inventory"}
var environments = []string{"prod", "staging"}
var users = []string{"u_1029", "u_2031", "u_3492", "u_4471", "u_5503"}

var messages = map[string][]string{
	"INFO": {
		"request completed",
		"user logged in",
		"order placed",
		"cache warmed",
		"checkout started",
		"payment authorized",
	},
	"WARN": {
		"slow query detected",
		"retrying upstream call",
		"circuit breaker half-open",
		"deprecated endpoint hit",
	},
	"ERROR": {
		"database connection refused",
		"payment gateway timeout",
		"inventory out of sync",
		"authentication failed",
		"panic in request handler",
		"upstream returned 502",
	},
}

func generateLogs(seed time.Time, host string) []openobserve.LogEntry {
	r := rand.New(rand.NewSource(42))
	out := make([]openobserve.LogEntry, 0, 500)
	for i := 0; i < 500; i++ {
		level := pickLevel(r)
		status := pickStatus(r, level)
		svc := services[r.Intn(len(services))]
		msgs := messages[level]
		msg := msgs[r.Intn(len(msgs))]
		ts := seed.Add(-time.Duration(r.Intn(3600)) * time.Second)
		traceID := fmt.Sprintf("trace_%08x", r.Intn(100000))
		dur := 20 + r.Intn(2000) // 20ms - 2s
		if level == "ERROR" {
			dur += 500
		}
		entry := openobserve.LogEntry{
			"_timestamp":  ts.UnixMicro(),
			"level":       level,
			"service":     svc,
			"environment": environments[r.Intn(len(environments))],
			"host":        fmt.Sprintf("%s-%d", host, r.Intn(4)+1),
			"method":      "POST",
			"path":        pickPath(r, svc),
			"status":      status,
			"duration_ms": dur,
			"trace_id":    traceID,
			"span_id":     fmt.Sprintf("span_%08x", r.Intn(100000)),
			"request_id":  fmt.Sprintf("req_%08x", i),
			"user_id":     users[r.Intn(len(users))],
			"message":     msg,
		}
		if level == "ERROR" {
			entry["error"] = pickError(r)
		}
		out = append(out, entry)
	}
	return out
}

func pickLevel(r *rand.Rand) string {
	x := r.Float64()
	switch {
	case x < 0.7:
		return "INFO"
	case x < 0.85:
		return "WARN"
	default:
		return "ERROR"
	}
}

func pickStatus(r *rand.Rand, level string) string {
	if level == "ERROR" {
		x := r.Float64()
		switch {
		case x < 0.6:
			return "500"
		case x < 0.85:
			return "503"
		case x < 0.95:
			return "502"
		default:
			return "401"
		}
	}
	x := r.Float64()
	switch {
	case x < 0.7:
		return "200"
	case x < 0.85:
		return "201"
	case x < 0.93:
		return "400"
	case x < 0.97:
		return "404"
	default:
		return "429"
	}
}

func pickPath(r *rand.Rand, svc string) string {
	switch svc {
	case "checkout":
		paths := []string{"/checkout", "/checkout/cart", "/checkout/confirm"}
		return paths[r.Intn(len(paths))]
	case "payment":
		paths := []string{"/payment/charge", "/payment/refund", "/payment/intent"}
		return paths[r.Intn(len(paths))]
	case "auth":
		paths := []string{"/auth/login", "/auth/logout", "/auth/refresh"}
		return paths[r.Intn(len(paths))]
	case "inventory":
		paths := []string{"/inventory/list", "/inventory/reserve", "/inventory/release"}
		return paths[r.Intn(len(paths))]
	default:
		paths := []string{"/api/v1/users", "/api/v1/orders", "/api/v1/products"}
		return paths[r.Intn(len(paths))]
	}
}

func pickError(r *rand.Rand) string {
	errs := []string{
		"connection refused",
		"context deadline exceeded",
		"unexpected EOF",
		"insufficient funds",
		"invalid authentication token",
		"database is unavailable",
		"panic: runtime error: index out of range",
	}
	return errs[r.Intn(len(errs))]
}

func generateMetrics(seed time.Time, host string) []map[string]any {
	r := rand.New(rand.NewSource(99))
	out := make([]map[string]any, 0, 120)
	metricNames := []string{
		"http_requests_total",
		"http_request_duration_ms",
		"http_errors_total",
		"cpu_usage",
		"memory_usage",
	}
	for i := 0; i < 120; i++ {
		svc := services[r.Intn(len(services))]
		metric := metricNames[r.Intn(len(metricNames))]
		var value float64
		switch metric {
		case "http_requests_total":
			value = float64(50 + r.Intn(500))
		case "http_request_duration_ms":
			value = 20 + float64(r.Intn(2000))
		case "http_errors_total":
			value = float64(r.Intn(15))
		case "cpu_usage":
			value = 0.1 + r.Float64()*0.8
		case "memory_usage":
			value = 0.2 + r.Float64()*0.6
		}
		ts := seed.Add(-time.Duration(r.Intn(3600)) * time.Second)
		out = append(out, map[string]any{
			"_timestamp":  ts.UnixMicro(),
			"metric_name": metric,
			"service":     svc,
			"environment": environments[r.Intn(len(environments))],
			"host":        fmt.Sprintf("%s-%d", host, r.Intn(4)+1),
			"value":       value,
		})
	}
	return out
}

func generateSpans(seed time.Time, host string) []openobserve.TraceSpan {
	r := rand.New(rand.NewSource(7))
	out := make([]openobserve.TraceSpan, 0, 60)
	for i := 0; i < 30; i++ {
		traceID := fmt.Sprintf("trace_%08x", i+1)
		parentSvc := "api"
		childSvc := services[r.Intn(len(services))]
		if childSvc == "api" {
			childSvc = "checkout"
		}
		ts := seed.Add(-time.Duration(r.Intn(3600)) * time.Second)
		parentSpanID := fmt.Sprintf("span_%08d", i*10+1)
		childSpanID := fmt.Sprintf("span_%08d", i*10+2)
		parentDur := 50 + r.Intn(1500)
		childDur := parentDur / 3
		status := "ok"
		if r.Float64() < 0.2 {
			status = "error"
		}

		out = append(out,
			openobserve.TraceSpan{
				"_timestamp":     ts.UnixMicro(),
				"trace_id":       traceID,
				"span_id":        parentSpanID,
				"parent_span_id": "",
				"service":        parentSvc,
				"operation":      fmt.Sprintf("POST %s", pickPath(r, parentSvc)),
				"duration":       parentDur * 1000,
				"status":         status,
				"host":           fmt.Sprintf("%s-%d", host, r.Intn(4)+1),
			},
			openobserve.TraceSpan{
				"_timestamp":     ts.Add(time.Duration(parentDur/2) * time.Millisecond).UnixMicro(),
				"trace_id":       traceID,
				"span_id":        childSpanID,
				"parent_span_id": parentSpanID,
				"service":        childSvc,
				"operation":      fmt.Sprintf("POST %s", pickPath(r, childSvc)),
				"duration":       childDur * 1000,
				"status":         status,
				"host":           fmt.Sprintf("%s-%d", host, r.Intn(4)+1),
			},
		)
	}
	return out
}
