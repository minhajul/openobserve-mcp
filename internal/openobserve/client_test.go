package openobserve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/puku/openobserve-mcp/internal/config"
)

type fakeTransport struct {
	queue   []*http.Response
	err     error
	gotReq  *http.Request
	gotBody []byte
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.gotBody = b
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.queue) == 0 {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r, nil
}

func newTestClient(t *testing.T, ft *fakeTransport) *Client {
	t.Helper()
	cfg := &config.Config{
		OpenObserveURL:      "http://test:5080",
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	return NewClientWithTransport(cfg, ft)
}

func jsonResp(t *testing.T, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(append([]byte{}, b...))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func schemaResp(t *testing.T, streamName string) *http.Response {
	t.Helper()
	return jsonResp(t, map[string]any{
		"list": []map[string]any{
			{
				"name": streamName,
				"schema": []map[string]any{
					{"name": "timestamp"},
					{"name": "level"},
					{"name": "service"},
					{"name": "message"},
				},
			},
		},
	})
}

func TestResolveStreamDefaults(t *testing.T) {
	c := &Client{}
	cases := []struct {
		kind  StreamKind
		given string
		want  string
	}{
		{KindLog, "", "default"},
		{KindMetric, "", "metrics"},
		{KindTrace, "", "traces"},
		{KindLog, "custom", "custom"},
		{KindMetric, "custom_metrics", "custom_metrics"},
	}
	for _, tc := range cases {
		got := c.resolveStream(tc.kind, tc.given)
		if got != tc.want {
			t.Errorf("resolveStream(%d, %q) = %q, want %q", tc.kind, tc.given, got, tc.want)
		}
	}
}

func TestSearchLogsDefaultsStreamAndRange(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		schemaResp(t, "default"),
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if ft.gotReq == nil {
		t.Fatal("transport not called")
	}
	if !strings.HasSuffix(ft.gotReq.URL.Path, "/api/default/_search") {
		t.Errorf("URL path = %q, want suffix /api/default/_search", ft.gotReq.URL.Path)
	}
	var body struct {
		Query struct {
			SQL string `json:"sql"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(body.Query.SQL, "SELECT timestamp, level, service, message FROM default WHERE ") {
		t.Errorf("SQL prefix unexpected: %q", body.Query.SQL)
	}
	if !strings.Contains(body.Query.SQL, "timestamp >= ") {
		t.Errorf("SQL missing timestamp >= clause: %q", body.Query.SQL)
	}
}

func TestSearchLogsSchemaDrivenColumns(t *testing.T) {
	streamsResp := jsonResp(t, map[string]any{
		"list": []map[string]any{
			{
				"name": "default",
				"schema": []map[string]any{
					{"name": "timestamp"},
					{"name": "level"},
					{"name": "service"},
					{"name": "message"},
					{"name": "status"},
					{"name": "duration_ms"},
				},
			},
		},
	})
	empty := jsonResp(t, map[string]any{"hits": []any{}})
	empty2 := jsonResp(t, map[string]any{"hits": []any{}})
	ft := &fakeTransport{queue: []*http.Response{streamsResp, empty, empty2}}

	c := newTestClient(t, ft)
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{}); err != nil {
		t.Fatalf("SearchLogs call 1 (streams): %v", err)
	}
	if _, err := c.SearchLogs(context.Background(), SearchLogsRequest{Service: "api"}); err != nil {
		t.Fatalf("SearchLogs call 2 (search): %v", err)
	}
	var body struct {
		Query struct {
			SQL string `json:"sql"`
		} `json:"query"`
	}
	if err := json.Unmarshal(ft.gotBody, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, want := range []string{"status", "duration_ms"} {
		if !strings.Contains(body.Query.SQL, want) {
			t.Errorf("SQL missing %q: %q", want, body.Query.SQL)
		}
	}
}

func TestAggregateLogsDefaultsGroupByRequired(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		jsonResp(t, map[string]any{"hits": []any{}}),
	}}
	c := newTestClient(t, ft)
	if _, err := c.AggregateLogs(context.Background(), AggregateLogsRequest{}); err == nil {
		t.Fatal("expected error when GroupBy is empty")
	}
}

func TestHealthy(t *testing.T) {
	ft := &fakeTransport{queue: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))},
	}}
	c := newTestClient(t, ft)
	if err := c.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
	if !strings.HasSuffix(ft.gotReq.URL.Path, "/health") {
		t.Errorf("Healthy path = %q, want suffix /health", ft.gotReq.URL.Path)
	}
}

func TestNewClientDefaultsTransport(t *testing.T) {
	cfg := &config.Config{
		OpenObserveURL:      "http://test:5080",
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	c := NewClient(cfg)
	if c.http == nil {
		t.Fatal("NewClient must initialize a non-nil Transport")
	}
}
