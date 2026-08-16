package openobserve

import (
	"context"

	"github.com/puku/openobserve-mcp/internal/sqlbuilder"
)

type LogEntry map[string]any

type SearchLogsRequest = sqlbuilder.SearchLogsRequest
type AggregateLogsRequest = sqlbuilder.AggregateLogsRequest
type TimeRange = sqlbuilder.TimeRange

var NowRange = sqlbuilder.NowRange

type SearchLogsResponse struct {
	Hits     []LogEntry `json:"hits"`
	Total    int64      `json:"total"`
	TookMs   int        `json:"took_ms"`
	QuerySQL string     `json:"query_sql"`
}

func (c *Client) SearchLogs(ctx context.Context, req SearchLogsRequest) (*SearchLogsResponse, error) {
	req.Stream = c.resolveStream(KindLog, req.Stream)
	sql, _, err := sqlbuilder.LogsBuild(ctx, req, c)
	if err != nil {
		return nil, err
	}
	body := searchBody(sql, req.Range, 0, req.Limit)
	resp := &SearchLogsResponse{QuerySQL: sql}
	if err := c.do(ctx, "POST", c.searchEndpoint(), body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) IngestLogs(ctx context.Context, stream string, entries []LogEntry) error {
	stream = c.resolveStream(KindLog, stream)
	if len(entries) == 0 {
		return nil
	}
	endpoint := c.ingestEndpoint(stream)
	return c.do(ctx, "POST", endpoint, entries, nil)
}

type AggregateLogsResponse struct {
	Groups   map[string]int64 `json:"groups"`
	Total    int64            `json:"total"`
	QuerySQL string           `json:"query_sql"`
}

func (c *Client) AggregateLogs(ctx context.Context, req AggregateLogsRequest) (*AggregateLogsResponse, error) {
	req.Stream = c.resolveStream(KindLog, req.Stream)
	sql, _, err := sqlbuilder.AggregateLogsBuild(req)
	if err != nil {
		return nil, err
	}
	body := searchBody(sql, req.Range, 0, 1000)
	var raw struct {
		Hits []struct {
			G string `json:"g"`
			C int64  `json:"c"`
		} `json:"hits"`
	}
	if err := c.do(ctx, "POST", c.searchEndpoint(), body, &raw); err != nil {
		return nil, err
	}
	out := &AggregateLogsResponse{
		Groups:   make(map[string]int64, len(raw.Hits)),
		QuerySQL: sql,
	}
	for _, h := range raw.Hits {
		out.Groups[h.G] = h.C
		out.Total += h.C
	}
	return out, nil
}
