package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/puku/openobserve-mcp/internal/openobserve"
)

// registerTools wires all observability tools into the MCP server.
func (s *Server) registerTools(srv *server.MCPServer) {
	srv.AddTool(s.searchLogsTool(), s.handleSearchLogs)
	srv.AddTool(s.getRecentLogsTool(), s.handleGetRecentLogs)
	srv.AddTool(s.searchErrorsTool(), s.handleSearchErrors)
	srv.AddTool(s.queryMetricsTool(), s.handleQueryMetrics)
	srv.AddTool(s.getMetricTool(), s.handleGetMetric)
	srv.AddTool(s.searchTracesTool(), s.handleSearchTraces)
	srv.AddTool(s.getTraceTool(), s.handleGetTrace)
	srv.AddTool(s.getServiceErrorsTool(), s.handleGetServiceErrors)
	srv.AddTool(s.getSlowRequestsTool(), s.handleGetSlowRequests)
	srv.AddTool(s.getErrorSummaryTool(), s.handleGetErrorSummary)
}

// ----------------------------------------------------------------------------
// Tool schemas
// ----------------------------------------------------------------------------

func (s *Server) searchLogsTool() mcp.Tool {
	return mcp.NewTool("search_logs",
		mcp.WithDescription(`Search application logs in OpenObserve using structured filters.

Use this tool when the user asks about:
- recent application logs
- errors or warnings
- logs for a specific service
- HTTP status codes
- logs during a particular time range
- logs containing a particular message
- slow requests

Parameters are returned as a JSON array of matching log entries.`),
		mcp.WithString("service",
			mcp.Description("Filter by service name (e.g. 'payment', 'checkout', 'api'). Optional."),
		),
		mcp.WithString("level",
			mcp.Description("Filter by log level. One of: TRACE, DEBUG, INFO, WARN, ERROR, FATAL. Optional."),
		),
		mcp.WithString("status",
			mcp.Description("Filter by HTTP status code (e.g. '500', '404'). Optional."),
		),
		mcp.WithString("trace_id",
			mcp.Description("Filter by trace ID. Optional."),
		),
		mcp.WithString("message_contains",
			mcp.Description("Case-insensitive substring match against the message field. Optional."),
		),
		mcp.WithNumber("min_duration_ms",
			mcp.Description("Return only logs with duration_ms greater than or equal to this value. Optional."),
		),
		mcp.WithString("since",
			mcp.Description("Time window relative to now (e.g. '15m', '1h', '24h'). Default: '1h'."),
		),
		mcp.WithString("start_time",
			mcp.Description("RFC3339 start time. Overrides 'since' if provided."),
		),
		mcp.WithString("end_time",
			mcp.Description("RFC3339 end time. Defaults to now."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of entries to return. Default 100, max 1000."),
			mcp.Min(1), mcp.Max(1000), mcp.DefaultNumber(100),
		),
		mcp.WithString("stream",
			mcp.Description("OpenObserve stream name. Default 'default'."),
		),
	)
}

func (s *Server) getRecentLogsTool() mcp.Tool {
	return mcp.NewTool("get_recent_logs",
		mcp.WithDescription(`Return the most recent log entries, optionally filtered by service.

Use this when the user asks things like 'show me the latest errors' or
'what just happened in the api service'.`),
		mcp.WithString("service",
			mcp.Description("Filter by service name. Optional."),
		),
		mcp.WithString("level",
			mcp.Description("Filter by log level. Optional."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Number of entries to return. Default 20."),
			mcp.Min(1), mcp.Max(500), mcp.DefaultNumber(20),
		),
	)
}

func (s *Server) searchErrorsTool() mcp.Tool {
	return mcp.NewTool("search_errors",
		mcp.WithDescription(`Search for error-level logs.

Use this when the user asks 'show me errors', 'what is failing', or
'find recent exceptions'.`),
		mcp.WithString("service",
			mcp.Description("Filter by service name. Optional."),
		),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max entries. Default 100."),
			mcp.Min(1), mcp.Max(1000), mcp.DefaultNumber(100),
		),
	)
}

func (s *Server) queryMetricsTool() mcp.Tool {
	return mcp.NewTool("query_metrics",
		mcp.WithDescription(`Run an aggregation query against a metric.

Use this when the user asks about request rates, error rates, latency,
CPU or memory usage, or any other numeric metric.

Common aggregations: avg, sum, count, max, min, p95, p99.`),
		mcp.WithString("metric",
			mcp.Description("Metric name (e.g. 'http_requests_total', 'http_request_duration_ms')."),
			mcp.Required(),
		),
		mcp.WithString("aggregation",
			mcp.Description("Aggregation function. One of: avg, sum, count, max, min, p95, p99. Default: avg."),
			mcp.Enum("avg", "sum", "count", "max", "min", "p95", "p99"),
		),
		mcp.WithString("service",
			mcp.Description("Filter by service. Optional."),
		),
		mcp.WithString("group_by",
			mcp.Description("Comma-separated list of fields to group by (e.g. 'service,status'). Optional."),
		),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max groups. Default 100."),
			mcp.Min(1), mcp.Max(1000), mcp.DefaultNumber(100),
		),
	)
}

func (s *Server) getMetricTool() mcp.Tool {
	return mcp.NewTool("get_metric",
		mcp.WithDescription(`Get recent samples for a specific metric.

Use this when the user wants the raw values of a metric across a time
window (as opposed to an aggregated value).`),
		mcp.WithString("metric",
			mcp.Description("Metric name."),
			mcp.Required(),
		),
		mcp.WithString("service",
			mcp.Description("Filter by service. Optional."),
		),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max samples. Default 100."),
			mcp.Min(1), mcp.Max(1000), mcp.DefaultNumber(100),
		),
	)
}

func (s *Server) searchTracesTool() mcp.Tool {
	return mcp.NewTool("search_traces",
		mcp.WithDescription(`Search for spans (traces) matching service/operation/status filters.

Use this when the user asks about traces, distributed requests, or
'show me failed requests'.`),
		mcp.WithString("service",
			mcp.Description("Filter by service name."),
		),
		mcp.WithString("operation",
			mcp.Description("Filter by operation name (e.g. 'POST /checkout')."),
		),
		mcp.WithString("status",
			mcp.Description("Filter by span status. 'ok' or 'error'."),
			mcp.Enum("ok", "error"),
		),
		mcp.WithString("trace_id",
			mcp.Description("Filter by trace ID."),
		),
		mcp.WithNumber("min_duration_ms",
			mcp.Description("Return only spans with duration >= this many milliseconds."),
		),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max spans. Default 100."),
			mcp.Min(1), mcp.Max(1000), mcp.DefaultNumber(100),
		),
	)
}

func (s *Server) getTraceTool() mcp.Tool {
	return mcp.NewTool("get_trace",
		mcp.WithDescription(`Retrieve all spans for a single trace ID.

Use this when the user wants to see the full call graph for a specific
request.`),
		mcp.WithString("trace_id",
			mcp.Description("The trace ID to look up."),
			mcp.Required(),
		),
	)
}

func (s *Server) getServiceErrorsTool() mcp.Tool {
	return mcp.NewTool("get_service_errors",
		mcp.WithDescription(`Count error entries per service within a time window.

Use this when the user asks 'which service has the most errors' or
'give me error counts by service'.`),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max services to return. Default 20."),
			mcp.Min(1), mcp.Max(100), mcp.DefaultNumber(20),
		),
	)
}

func (s *Server) getSlowRequestsTool() mcp.Tool {
	return mcp.NewTool("get_slow_requests",
		mcp.WithDescription(`Return the slowest HTTP requests in the time window.

Use this when the user asks about slow requests, latency, or p95/p99.`),
		mcp.WithString("service",
			mcp.Description("Filter by service. Optional."),
		),
		mcp.WithNumber("min_duration_ms",
			mcp.Description("Minimum duration_ms threshold. Default 1000."),
			mcp.Min(0), mcp.DefaultNumber(1000),
		),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max entries. Default 20."),
			mcp.Min(1), mcp.Max(500), mcp.DefaultNumber(20),
		),
	)
}

func (s *Server) getErrorSummaryTool() mcp.Tool {
	return mcp.NewTool("get_error_summary",
		mcp.WithDescription(`Summarize errors: total count, per-service counts, and HTTP status
breakdown for the time window.

Use this when the user wants a high-level error overview.`),
		mcp.WithString("since",
			mcp.Description("Time window. Default '1h'."),
		),
	)
}

// ----------------------------------------------------------------------------
// Tool handlers
// ----------------------------------------------------------------------------

func (s *Server) handleSearchLogs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	defer func() { s.logToolCall("search_logs", start, nil) }()

	args := req.GetArguments()
	r := openobserve.SearchLogsRequest{
		Stream:          stringArg(args, "stream", "default"),
		Service:         stringArg(args, "service", ""),
		Level:           stringArg(args, "level", ""),
		Status:          stringArg(args, "status", ""),
		TraceID:         stringArg(args, "trace_id", ""),
		MessageContains: stringArg(args, "message_contains", ""),
	}

	if v, ok := args["min_duration_ms"].(float64); ok {
		r.MinDurationMS = int(v)
	}
	if v, ok := args["limit"].(float64); ok {
		r.Limit = int(v)
	}

	startT, endT, err := resolveTimeWindow(
		stringArg(args, "start_time", ""),
		stringArg(args, "end_time", ""),
		stringArg(args, "since", "1h"),
	)
	if err != nil {
		return errorResult(err), nil
	}
	r.StartTime, r.EndTime = startT, endT

	resp, err := s.client.SearchLogs(ctx, r)
	if err != nil {
		s.logToolCall("search_logs", start, err)
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleGetRecentLogs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	limit := int(floatArg(args, "limit", 20))
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Limit:     limit,
		Service:   stringArg(args, "service", ""),
		Level:     stringArg(args, "level", ""),
		Stream:    "default",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
	})
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleSearchErrors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	limit := int(floatArg(args, "limit", 100))
	since := stringArg(args, "since", "1h")
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:    "default",
		Service:   stringArg(args, "service", ""),
		Level:     "ERROR",
		Limit:     limit,
		StartTime: startT,
		EndTime:   endT,
	})
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleQueryMetrics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	metric := stringArg(args, "metric", "")
	if metric == "" {
		return errorResult(fmt.Errorf("metric is required")), nil
	}
	agg := strings.ToLower(stringArg(args, "aggregation", "avg"))
	groupBy := splitCSV(stringArg(args, "group_by", ""))
	limit := int(floatArg(args, "limit", 100))
	since := stringArg(args, "since", "1h")
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}

	resp, err := s.client.QueryMetrics(ctx, openobserve.QueryMetricsRequest{
		MetricName:  metric,
		Aggregation: agg,
		Service:     stringArg(args, "service", ""),
		GroupBy:     groupBy,
		StartTime:   startT,
		EndTime:     endT,
		Limit:       limit,
	})
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleGetMetric(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// For a single metric, return count samples as a stand-in.
	args := req.GetArguments()
	metric := stringArg(args, "metric", "")
	if metric == "" {
		return errorResult(fmt.Errorf("metric is required")), nil
	}
	since := stringArg(args, "since", "1h")
	limit := int(floatArg(args, "limit", 100))
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}
	resp, err := s.client.QueryMetrics(ctx, openobserve.QueryMetricsRequest{
		MetricName:  metric,
		Aggregation: "avg",
		Service:     stringArg(args, "service", ""),
		StartTime:   startT,
		EndTime:     endT,
		Limit:       limit,
	})
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleSearchTraces(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	limit := int(floatArg(args, "limit", 100))
	since := stringArg(args, "since", "1h")
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}
	r := openobserve.SearchTracesRequest{
		Stream:    "traces",
		Service:   stringArg(args, "service", ""),
		Operation: stringArg(args, "operation", ""),
		Status:    stringArg(args, "status", ""),
		TraceID:   stringArg(args, "trace_id", ""),
		Limit:     limit,
		StartTime: startT,
		EndTime:   endT,
	}
	if v, ok := args["min_duration_ms"].(float64); ok {
		r.MinSpanDur = int(v) * 1000 // ms -> us
	}
	resp, err := s.client.SearchTraces(ctx, r)
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleGetTrace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	traceID := stringArg(args, "trace_id", "")
	if traceID == "" {
		return errorResult(fmt.Errorf("trace_id is required")), nil
	}
	resp, err := s.client.GetTrace(ctx, "traces", traceID)
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleGetServiceErrors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	since := stringArg(args, "since", "1h")
	limit := int(floatArg(args, "limit", 20))
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}
	// Fetch up to `limit * 10` errors and tally per service. For very
	// high cardinality this would need SQL aggregation in OpenObserve,
	// but this matches typical "service errors" use cases.
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:    "default",
		Level:     "ERROR",
		Limit:     limit * 10,
		StartTime: startT,
		EndTime:   endT,
	})
	if err != nil {
		return errorResult(err), nil
	}
	counts := map[string]int{}
	for _, h := range resp.Hits {
		if svc, ok := h["service"].(string); ok {
			counts[svc]++
		}
	}
	return jsonResult(map[string]any{
		"total_errors": resp.Total,
		"by_service":   counts,
		"window":       map[string]any{"start": startT, "end": endT},
	})
}

func (s *Server) handleGetSlowRequests(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	minDuration := int(floatArg(args, "min_duration_ms", 1000))
	since := stringArg(args, "since", "1h")
	limit := int(floatArg(args, "limit", 20))
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}
	resp, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:        "default",
		Service:       stringArg(args, "service", ""),
		MinDurationMS: minDuration,
		Limit:         limit,
		StartTime:     startT,
		EndTime:       endT,
	})
	if err != nil {
		return errorResult(err), nil
	}
	return jsonResult(resp)
}

func (s *Server) handleGetErrorSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	since := stringArg(args, "since", "1h")
	startT, endT, err := resolveTimeWindow("", "", since)
	if err != nil {
		return errorResult(err), nil
	}

	// Fetch error logs and aggregate per service / status.
	logs, err := s.client.SearchLogs(ctx, openobserve.SearchLogsRequest{
		Stream:    "default",
		Level:     "ERROR",
		Limit:     500,
		StartTime: startT,
		EndTime:   endT,
	})
	if err != nil {
		return errorResult(err), nil
	}

	byService := map[string]int{}
	byStatus := map[string]int{}
	for _, h := range logs.Hits {
		if svc, ok := h["service"].(string); ok {
			byService[svc]++
		}
		if st, ok := h["status"].(string); ok {
			byStatus[st]++
		}
	}

	return jsonResult(map[string]any{
		"total_errors": logs.Total,
		"by_service":   byService,
		"by_status":    byStatus,
		"window":       map[string]any{"start": startT, "end": endT},
	})
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

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
