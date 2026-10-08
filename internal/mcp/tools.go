package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/puku/openobserve-mcp/internal/openobserve"
)

// Logical data sets. These names stay server-side; clients never pass them.
const (
	logsStream    = "default"
	metricsStream = "metrics"
	tracesStream  = "traces"
)

func (s *Server) registerTools(srv *server.MCPServer) {
	srv.AddTool(s.searchLogsTool(), s.handle("search_logs", s.searchLogs))
	srv.AddTool(s.getRecentLogsTool(), s.handle("get_recent_logs", s.getRecentLogs))
	srv.AddTool(s.searchErrorsTool(), s.handle("search_errors", s.searchErrors))
	srv.AddTool(s.queryMetricsTool(), s.handle("query_metrics", s.queryMetrics))
	srv.AddTool(s.getMetricTool(), s.handle("get_metric", s.getMetric))
	srv.AddTool(s.listMetricsTool(), s.handle("list_metrics", s.listMetrics))
	srv.AddTool(s.listServicesTool(), s.handle("list_services", s.listServices))
	srv.AddTool(s.searchTracesTool(), s.handle("search_traces", s.searchTraces))
	srv.AddTool(s.getTraceTool(), s.handle("get_trace", s.getTrace))
	srv.AddTool(s.getServiceErrorsTool(), s.handle("get_service_errors", s.getServiceErrors))
	srv.AddTool(s.getSlowRequestsTool(), s.handle("get_slow_requests", s.getSlowRequests))
	srv.AddTool(s.getErrorSummaryTool(), s.handle("get_error_summary", s.getErrorSummary))
}

// toolFunc does a tool's work. It returns the JSON-able result plus the SQL it
// ran (for debug logging only; SQL is never returned to the client).
type toolFunc func(ctx context.Context, args map[string]any) (result any, sql []string, err error)

func (s *Server) handle(name string, fn toolFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		result, sql, err := fn(ctx, req.GetArguments())
		for _, q := range sql {
			s.logger.Debug("tool query", slog.String("tool", name), slog.String("sql", q))
		}
		s.logToolCall(name, start, err)
		if err != nil {
			return errorResult(err), nil
		}
		return jsonResult(result)
	}
}

// windowOptions are the time-window parameters shared by every windowed tool.
func windowOptions(defaultSince string) []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("since",
			mcp.Description(fmt.Sprintf("Time window relative to now (e.g. '15m', '1h', '24h', '7d'). Default: '%s'.", defaultSince)),
		),
		mcp.WithString("start_time",
			mcp.Description("RFC3339 start time. Overrides 'since' if provided."),
		),
		mcp.WithString("end_time",
			mcp.Description("RFC3339 end time. Defaults to now."),
		),
	}
}

func newTool(name, desc string, opts ...[]mcp.ToolOption) mcp.Tool {
	all := []mcp.ToolOption{mcp.WithDescription(desc)}
	for _, o := range opts {
		all = append(all, o...)
	}
	return mcp.NewTool(name, all...)
}

func limitOption(def, max int) mcp.ToolOption {
	return mcp.WithNumber("limit",
		mcp.Description(fmt.Sprintf("Maximum number of results. Default %d, max %d.", def, max)),
		mcp.Min(1), mcp.Max(float64(max)), mcp.DefaultNumber(float64(def)),
	)
}

var levelOption = mcp.WithString("level",
	mcp.Description("Filter by log level. Optional."),
	mcp.Enum("TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"),
)

func (s *Server) searchLogsTool() mcp.Tool {
	return newTool("search_logs", `Search application logs using structured filters.

Use this tool when the user asks about:
- recent application logs
- errors or warnings
- logs for a specific service
- HTTP status codes
- logs during a particular time range
- logs containing a particular message
- slow requests

Returns matching log entries, newest first.`,
		[]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("Filter by service name (e.g. 'payment', 'checkout', 'api'). Optional.")),
			levelOption,
			mcp.WithString("status", mcp.Description("Filter by HTTP status code (e.g. '500', '404'). Optional.")),
			mcp.WithString("trace_id", mcp.Description("Filter by trace ID. Optional.")),
			mcp.WithString("message_contains", mcp.Description("Case-insensitive substring match against the message field. Optional.")),
			mcp.WithNumber("min_duration_ms", mcp.Description("Only logs with duration_ms >= this value. Optional."), mcp.Min(0)),
			limitOption(100, 1000),
		},
		windowOptions("1h"),
	)
}

func (s *Server) getRecentLogsTool() mcp.Tool {
	return newTool("get_recent_logs", `Return the most recent log entries, optionally filtered by service or level.

Use this when the user asks things like 'what just happened in the api service'.`,
		[]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("Filter by service name. Optional.")),
			levelOption,
			limitOption(20, 500),
			mcp.WithString("since", mcp.Description("How far back to look (e.g. '15m', '1h', '24h'). Default: '1h'.")),
		},
	)
}

func (s *Server) searchErrorsTool() mcp.Tool {
	return newTool("search_errors", `Search for error-level logs.

Use this when the user asks 'show me errors', 'what is failing', or
'find recent exceptions'.`,
		[]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("Filter by service name. Optional.")),
			limitOption(100, 1000),
		},
		windowOptions("1h"),
	)
}

func (s *Server) queryMetricsTool() mcp.Tool {
	return newTool("query_metrics", `Run an aggregation over a metric.

Use this when the user asks about request rates, error rates, latency,
CPU or memory usage, or any other numeric metric. Call list_metrics first
if you don't know the metric name.`,
		[]mcp.ToolOption{
			mcp.WithString("metric", mcp.Description("Metric name (e.g. 'http_requests_total', 'http_request_duration_ms')."), mcp.Required()),
			mcp.WithString("aggregation",
				mcp.Description("Aggregation function. Default: avg."),
				mcp.Enum("avg", "sum", "count", "max", "min", "p50", "p90", "p95", "p99"),
			),
			mcp.WithString("service", mcp.Description("Filter by service. Optional.")),
			mcp.WithString("group_by", mcp.Description("Comma-separated fields to group by: service, environment, host. Optional.")),
			limitOption(100, 1000),
		},
		windowOptions("1h"),
	)
}

func (s *Server) getMetricTool() mcp.Tool {
	return newTool("get_metric", `Get the average value of a metric across a time window.

Use query_metrics for other aggregations (sum, p95, p99) or grouping.`,
		[]mcp.ToolOption{
			mcp.WithString("metric", mcp.Description("Metric name."), mcp.Required()),
			mcp.WithString("service", mcp.Description("Filter by service. Optional.")),
		},
		windowOptions("1h"),
	)
}

func (s *Server) listMetricsTool() mcp.Tool {
	return newTool("list_metrics", `List the metric names that have data in the time window, with sample counts.

Use this to discover valid metric names before calling query_metrics or get_metric.`,
		windowOptions("24h"),
	)
}

func (s *Server) listServicesTool() mcp.Tool {
	return newTool("list_services", `List the services that emitted logs in the time window, with log counts.

Use this to discover valid service names for the other tools' 'service' filter.`,
		windowOptions("24h"),
	)
}

func (s *Server) searchTracesTool() mcp.Tool {
	return newTool("search_traces", `Search spans matching service/operation/status filters.

Use this when the user asks about traces, distributed requests, or
'show me failed requests'. Span 'duration' is in microseconds.`,
		[]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("Filter by service name.")),
			mcp.WithString("operation", mcp.Description("Filter by operation name (e.g. 'POST /checkout').")),
			mcp.WithString("status", mcp.Description("Filter by span status."), mcp.Enum("ok", "error")),
			mcp.WithString("trace_id", mcp.Description("Filter by trace ID.")),
			mcp.WithNumber("min_duration_ms", mcp.Description("Only spans lasting at least this many milliseconds."), mcp.Min(0)),
			limitOption(100, 1000),
		},
		windowOptions("1h"),
	)
}

func (s *Server) getTraceTool() mcp.Tool {
	return newTool("get_trace", `Retrieve all spans for a single trace ID, in start-time order.

Use this when the user wants the full call graph for a specific request.
Span 'duration' is in microseconds.`,
		[]mcp.ToolOption{
			mcp.WithString("trace_id", mcp.Description("The trace ID to look up."), mcp.Required()),
			mcp.WithString("since", mcp.Description("How far back to search. Default: '7d'.")),
		},
	)
}

func (s *Server) getServiceErrorsTool() mcp.Tool {
	return newTool("get_service_errors", `Count error entries per service within a time window, most errors first.

Use this when the user asks 'which service has the most errors' or
'give me error counts by service'.`,
		[]mcp.ToolOption{limitOption(20, 100)},
		windowOptions("1h"),
	)
}

func (s *Server) getSlowRequestsTool() mcp.Tool {
	return newTool("get_slow_requests", `Return the slowest HTTP requests in the time window, slowest first.

Use this when the user asks about slow requests or latency outliers.`,
		[]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("Filter by service. Optional.")),
			mcp.WithNumber("min_duration_ms", mcp.Description("Minimum duration_ms threshold. Default 1000."), mcp.Min(0), mcp.DefaultNumber(1000)),
			limitOption(20, 500),
		},
		windowOptions("1h"),
	)
}

func (s *Server) getErrorSummaryTool() mcp.Tool {
	return newTool("get_error_summary", `Summarize errors: total count, per-service counts, and HTTP status
breakdown for the time window.

Use this when the user wants a high-level error overview.`,
		windowOptions("1h"),
	)
}

func (s *Server) searchLogs(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:          logsStream,
		Service:         stringArg(args, "service", ""),
		Level:           stringArg(args, "level", ""),
		Status:          stringArg(args, "status", ""),
		TraceID:         stringArg(args, "trace_id", ""),
		MessageContains: stringArg(args, "message_contains", ""),
		MinDurationMS:   int(floatArg(args, "min_duration_ms", 0)),
		Limit:           int(floatArg(args, "limit", 100)),
		Range:           rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, []string{resp.QuerySQL}, nil
}

func (s *Server) getRecentLogs(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(map[string]any{"since": stringArg(args, "since", "")}, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:  logsStream,
		Service: stringArg(args, "service", ""),
		Level:   stringArg(args, "level", ""),
		Limit:   int(floatArg(args, "limit", 20)),
		Range:   rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, []string{resp.QuerySQL}, nil
}

func (s *Server) searchErrors(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:  logsStream,
		Service: stringArg(args, "service", ""),
		Level:   "ERROR",
		Limit:   int(floatArg(args, "limit", 100)),
		Range:   rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, []string{resp.QuerySQL}, nil
}

func (s *Server) queryMetrics(ctx context.Context, args map[string]any) (any, []string, error) {
	metric := stringArg(args, "metric", "")
	if metric == "" {
		return nil, nil, fmt.Errorf("metric is required")
	}
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	agg := strings.ToLower(stringArg(args, "aggregation", "avg"))
	resp, err := s.client.QueryMetrics(ctx, openobserve.QueryMetricsRequest{
		Stream:      metricsStream,
		MetricName:  metric,
		Aggregation: agg,
		Service:     stringArg(args, "service", ""),
		GroupBy:     splitCSV(stringArg(args, "group_by", "")),
		Range:       rng,
		Limit:       int(floatArg(args, "limit", 100)),
	})
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"metric":      metric,
		"aggregation": agg,
		"results":     resp.Hits,
		"window":      rng,
	}, []string{resp.QuerySQL}, nil
}

func (s *Server) getMetric(ctx context.Context, args map[string]any) (any, []string, error) {
	metric := stringArg(args, "metric", "")
	if metric == "" {
		return nil, nil, fmt.Errorf("metric is required")
	}
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.QueryMetrics(ctx, openobserve.QueryMetricsRequest{
		Stream:      metricsStream,
		MetricName:  metric,
		Aggregation: "avg",
		Service:     stringArg(args, "service", ""),
		Range:       rng,
		Limit:       1,
	})
	if err != nil {
		return nil, nil, err
	}
	// avg over zero rows is SQL NULL; surface that as "no data", not 0.
	var value any
	if len(resp.Hits) > 0 {
		value = resp.Hits[0]["value"]
	}
	return map[string]any{
		"metric":      metric,
		"aggregation": "avg",
		"value":       value,
		"window":      rng,
	}, []string{resp.QuerySQL}, nil
}

func (s *Server) listMetrics(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "24h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.AggregateLogs(ctx, openobserve.AggregateLogsRequest{
		Stream:  metricsStream,
		GroupBy: "metric_name",
		Range:   rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{"metrics": resp.Groups, "window": rng}, []string{resp.QuerySQL}, nil
}

func (s *Server) listServices(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "24h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.AggregateLogs(ctx, openobserve.AggregateLogsRequest{
		Stream:  logsStream,
		GroupBy: "service",
		Range:   rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{"services": resp.Groups, "window": rng}, []string{resp.QuerySQL}, nil
}

func (s *Server) searchTraces(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.SearchTraces(ctx, openobserve.SearchTracesRequest{
		Stream:     tracesStream,
		Service:    stringArg(args, "service", ""),
		Operation:  stringArg(args, "operation", ""),
		Status:     stringArg(args, "status", ""),
		TraceID:    stringArg(args, "trace_id", ""),
		MinSpanDur: int(floatArg(args, "min_duration_ms", 0)) * 1000, // ms -> µs
		Limit:      int(floatArg(args, "limit", 100)),
		Range:      rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, []string{resp.QuerySQL}, nil
}

func (s *Server) getTrace(ctx context.Context, args map[string]any) (any, []string, error) {
	traceID := stringArg(args, "trace_id", "")
	if traceID == "" {
		return nil, nil, fmt.Errorf("trace_id is required")
	}
	rng, err := windowFromArgs(map[string]any{"since": stringArg(args, "since", "")}, "7d")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.GetTrace(ctx, tracesStream, traceID, rng)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"trace_id":   traceID,
		"span_count": resp.Count,
		"spans":      resp.Hits,
	}, []string{resp.QuerySQL}, nil
}

func (s *Server) errorsBy(ctx context.Context, field string, rng openobserve.TimeRange) (*openobserve.AggregateLogsResponse, error) {
	return s.client.AggregateLogs(ctx, openobserve.AggregateLogsRequest{
		Stream:  logsStream,
		GroupBy: field,
		Where:   []string{"level = 'ERROR'"},
		Range:   rng,
	})
}

func (s *Server) getServiceErrors(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.errorsBy(ctx, "service", rng)
	if err != nil {
		return nil, nil, err
	}
	groups := resp.Groups
	if limit := int(floatArg(args, "limit", 20)); limit > 0 && len(groups) > limit {
		groups = groups[:limit]
	}
	return map[string]any{
		"total_errors": resp.Total,
		"by_service":   groups,
		"window":       rng,
	}, []string{resp.QuerySQL}, nil
}

func (s *Server) getSlowRequests(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:        logsStream,
		Service:       stringArg(args, "service", ""),
		MinDurationMS: int(floatArg(args, "min_duration_ms", 1000)),
		OrderBy:       openobserve.OrderByDuration,
		Limit:         int(floatArg(args, "limit", 20)),
		Range:         rng,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, []string{resp.QuerySQL}, nil
}

func (s *Server) getErrorSummary(ctx context.Context, args map[string]any) (any, []string, error) {
	rng, err := windowFromArgs(args, "1h")
	if err != nil {
		return nil, nil, err
	}
	byService, err := s.errorsBy(ctx, "service", rng)
	if err != nil {
		return nil, nil, err
	}
	byStatus, err := s.errorsBy(ctx, "status", rng)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"total_errors": byService.Total,
		"by_service":   byService.Groups,
		"by_status":    byStatus.Groups,
		"window":       rng,
	}, []string{byService.QuerySQL, byStatus.QuerySQL}, nil
}
