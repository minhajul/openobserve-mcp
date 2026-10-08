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

const OrderByDuration = sqlbuilder.OrderByDuration

type SearchLogsResponse struct {
	Hits   []LogEntry `json:"hits"`
	Count  int        `json:"count"`
	TookMs int        `json:"took_ms"`
	// QuerySQL is kept for server-side logging; it is never serialized to MCP clients.
	QuerySQL string `json:"-"`
}

func (c *Client) SearchLogs(ctx context.Context, req SearchLogsRequest) (*SearchLogsResponse, error) {
	req.Stream = c.resolveStream(KindLog, req.Stream)
	req.Range = req.Range.Normalize()
	sql, _, err := sqlbuilder.LogsBuild(ctx, req, c)
	if err != nil {
		return nil, err
	}
	res, err := c.search(ctx, sql, req.Range)
	if err != nil {
		return nil, err
	}
	resp := &SearchLogsResponse{Hits: make([]LogEntry, len(res.Hits)), Count: len(res.Hits), TookMs: res.Took, QuerySQL: sql}
	for i, h := range res.Hits {
		resp.Hits[i] = h
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

type AggregateGroup struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type AggregateLogsResponse struct {
	// Groups is ordered by Count, descending.
	Groups   []AggregateGroup `json:"groups"`
	Total    int64            `json:"total"`
	QuerySQL string           `json:"-"`
}

func (c *Client) AggregateLogs(ctx context.Context, req AggregateLogsRequest) (*AggregateLogsResponse, error) {
	req.Stream = c.resolveStream(KindLog, req.Stream)
	req.Range = req.Range.Normalize()
	sql, _, err := sqlbuilder.AggregateLogsBuild(req)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Hits []struct {
			G *string `json:"g"`
			C int64   `json:"c"`
		} `json:"hits"`
	}
	if err := c.do(ctx, "POST", c.searchEndpoint(), searchBody(sql, req.Range, 0, searchPageSize), &raw); err != nil {
		return nil, err
	}
	out := &AggregateLogsResponse{
		Groups:   make([]AggregateGroup, 0, len(raw.Hits)),
		QuerySQL: sql,
	}
	for _, h := range raw.Hits {
		key := "(none)"
		if h.G != nil && *h.G != "" {
			key = *h.G
		}
		out.Groups = append(out.Groups, AggregateGroup{Key: key, Count: h.C})
		out.Total += h.C
	}
	return out, nil
}
