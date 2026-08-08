// Package openobserve implements a thin HTTP client for OpenObserve.
//
// The MCP layer interacts only with the types and methods exposed here. The
// rest of the project must not construct OpenObserve URLs, headers, or
// query payloads directly.
package openobserve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/puku/openobserve-mcp/internal/config"
)

// schemaTTL bounds how long a cached stream schema is considered fresh.
// Stream schemas change rarely; a generous TTL avoids a per-request
// round-trip to /api/{org}/streams.
const schemaTTL = 5 * time.Minute

// Client talks to OpenObserve over HTTP. It is safe for concurrent use.
type Client struct {
	cfg     *config.Config
	http    *http.Client
	baseURL string

	schemaMu    sync.RWMutex
	schemaCache map[string]schemaEntry
}

type schemaEntry struct {
	fields    map[string]bool
	expiresAt time.Time
}

// NewClient creates a Client. The HTTP client uses the configured timeout.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.OpenObserveTimeout,
		},
		baseURL:     cfg.OpenObserveURL,
		schemaCache: make(map[string]schemaEntry),
	}
}

// Healthy returns true if OpenObserve's /healthz endpoint responds 200.
func (c *Client) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("openobserve not healthy: status %d", resp.StatusCode)
	}
	return nil
}

// do executes an HTTP request with auth headers and JSON decoding.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.cfg.OpenObserveUsername, c.cfg.OpenObservePassword)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openobserve request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20)) // 25 MiB cap
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       truncate(string(respBody), 4096),
		}
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w (status %d, body %s)", err, resp.StatusCode, truncate(string(respBody), 256))
		}
	}
	return nil
}

// APIError is returned for non-2xx responses from OpenObserve.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openobserve api error: status %d, body %s", e.StatusCode, truncate(e.Body, 256))
}

// IsUnauthorized reports whether the error is an authentication failure.
func (e *APIError) IsUnauthorized() bool { return e.StatusCode == 401 || e.StatusCode == 403 }

// IsNotFound reports whether the error is a missing resource.
func (e *APIError) IsNotFound() bool { return e.StatusCode == 404 }

// truncate shortens s to at most n bytes for logging.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// escape escapes a value for embedding inside a SQL string literal.
func escape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return v
}

// streamSchema returns the set of fields registered for the named
// stream. Used to build queries that SELECT only existing columns.
//
// Results are cached for schemaTTL — stream schemas change rarely.
func (c *Client) streamSchema(ctx context.Context, stream string) (map[string]bool, error) {
	if fields, ok := c.cachedSchema(stream); ok {
		return fields, nil
	}
	endpoint := fmt.Sprintf("/api/%s/streams",
		url.PathEscape(c.cfg.OpenObserveOrg))
	var resp struct {
		List []struct {
			Name   string `json:"name"`
			Schema []struct {
				Name string `json:"name"`
			} `json:"schema"`
		} `json:"list"`
	}
	if err := c.do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	for _, s := range resp.List {
		if s.Name == stream {
			out := make(map[string]bool, len(s.Schema))
			for _, f := range s.Schema {
				out[f.Name] = true
			}
			c.storeSchema(stream, out)
			return out, nil
		}
	}
	return nil, fmt.Errorf("stream %q not found", stream)
}

func (c *Client) cachedSchema(stream string) (map[string]bool, bool) {
	c.schemaMu.RLock()
	defer c.schemaMu.RUnlock()
	e, ok := c.schemaCache[stream]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.fields, true
}

func (c *Client) storeSchema(stream string, fields map[string]bool) {
	c.schemaMu.Lock()
	defer c.schemaMu.Unlock()
	c.schemaCache[stream] = schemaEntry{fields: fields, expiresAt: time.Now().Add(schemaTTL)}
}
