// Package agent implements a small LLM agent that connects to the MCP
// server over stdio, exposes the discovered tools to a model, and
// returns a natural-language answer.
//
// The agent has no knowledge of OpenObserve. It talks only to the MCP
// server.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/puku/openobserve-mcp/internal/config"
)

// Agent is the high-level entry point.
type Agent struct {
	cfg    *config.Config
	logger *slog.Logger
	mcpBin string
}

// Options configure the Agent.
type Options struct {
	Logger *slog.Logger
	MCPBin string // explicit path to mcp-server binary
}

// New creates an Agent.
func New(cfg *config.Config, opts Options) *Agent {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Agent{cfg: cfg, logger: opts.Logger, mcpBin: opts.MCPBin}
}

// Run connects to the MCP server, lists tools, asks the model, and
// prints the final natural-language answer.
func (a *Agent) Run(ctx context.Context, question string) error {
	if a.cfg.AnthropicAPIKey == "" {
		return errors.New("ANTHROPIC_API_KEY is required for the agent")
	}

	mcpClient, err := a.startMCPClient(ctx)
	if err != nil {
		return fmt.Errorf("start mcp: %w", err)
	}
	defer func() {
		_ = mcpClient.Close()
	}()

	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "openobserve-mcp-agent",
				Version: "0.1.0",
			},
		},
	}); err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}

	toolsResp, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	a.logger.Info("connected to mcp", slog.Int("tools", len(toolsResp.Tools)))

	providerTools := toProviderTools(toolsResp.Tools)
	answer, err := a.runConversation(ctx, question, providerTools, mcpClient)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

func (a *Agent) startMCPClient(ctx context.Context) (*mcpclient.Client, error) {
	bin := a.mcpBin
	if bin == "" {
		bin = os.Getenv("MCP_SERVER_BIN")
	}
	if bin == "" {
		return nil, errors.New("MCP server binary path is required; set MCP_SERVER_BIN or pass Options.MCPBin")
	}

	env := append(os.Environ(),
		"OPENOBSERVE_URL="+a.cfg.OpenObserveURL,
		"OPENOBSERVE_ORG="+a.cfg.OpenObserveOrg,
		"OPENOBSERVE_USERNAME="+a.cfg.OpenObserveUsername,
		"OPENOBSERVE_PASSWORD="+a.cfg.OpenObservePassword,
		"OPENOBSERVE_TIMEOUT="+a.cfg.OpenObserveTimeout.String(),
	)

	c, err := mcpclient.NewStdioMCPClientWithOptions(
		bin,
		env,
		nil,
		transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = env
			cmd.Stderr = io.Discard
			return cmd, nil
		}),
	)
	if err != nil {
		return nil, err
	}
	_ = ctx
	return c, nil
}

// runConversation runs the tool-use loop against Anthropic.
func (a *Agent) runConversation(ctx context.Context, question string, tools []providerTool, mcpClient *mcpclient.Client) (string, error) {
	messages := []providerMessage{
		{Role: "user", Content: []providerContent{{Type: "text", Text: question}}},
	}
	for turn := 0; turn < 8; turn++ {
		resp, err := a.callModel(ctx, messages, tools)
		if err != nil {
			return "", err
		}
		assistantMsg := providerMessage{Role: "assistant", Content: []providerContent{}}
		for _, blk := range resp.Content {
			switch blk.Type {
			case "text":
				assistantMsg.Content = append(assistantMsg.Content, providerContent{Type: "text", Text: blk.Text})
			case "tool_use":
				assistantMsg.Content = append(assistantMsg.Content, providerContent{
					Type: "tool_use", ID: blk.ID, Name: blk.Name, Input: blk.Input,
				})
			}
		}
		messages = append(messages, assistantMsg)

		if resp.StopReason != "tool_use" {
			return resp.Text(), nil
		}

		for _, blk := range resp.Content {
			if blk.Type != "tool_use" {
				continue
			}
			a.logger.Info("tool call",
				slog.String("tool", blk.Name),
				slog.Any("input", blk.Input),
			)
			res, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      blk.Name,
					Arguments: blk.Input,
				},
			})
			if err != nil {
				return "", fmt.Errorf("tool %s failed: %w", blk.Name, err)
			}
			text := resultToText(res)
			messages = append(messages, providerMessage{
				Role: "user",
				Content: []providerContent{{
					Type:      "tool_result",
					ToolUseID: blk.ID,
					Content:   text,
					IsError:   res.IsError,
				}},
			})
		}
	}
	return "", errors.New("exceeded tool-use turns")
}

func resultToText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := mcp.AsTextContent(c); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 {
		return "(no text content)"
	}
	return b.String()
}

// ----------------------------------------------------------------------------
// Anthropic Messages API (minimal subset).
// ----------------------------------------------------------------------------

type providerTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type providerMessage struct {
	Role    string            `json:"role"`
	Content []providerContent `json:"content"`
}

type providerContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type providerResponse struct {
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text,omitempty"`
		ID    string         `json:"id,omitempty"`
		Name  string         `json:"name,omitempty"`
		Input map[string]any `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (r *providerResponse) Text() string {
	var s string
	for _, c := range r.Content {
		if c.Type == "text" {
			s += c.Text
		}
	}
	return s
}

func toProviderTools(in []mcp.Tool) []providerTool {
	out := make([]providerTool, 0, len(in))
	for _, t := range in {
		// Marshal ToolInputSchema to JSON and back to a map[string]any so
		// it has the exact shape the Anthropic API expects.
		raw, err := json.Marshal(t.InputSchema)
		if err != nil {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			continue
		}
		out = append(out, providerTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}

func (a *Agent) callModel(ctx context.Context, messages []providerMessage, tools []providerTool) (*providerResponse, error) {
	body := map[string]any{
		"model":      a.cfg.AnthropicModel,
		"max_tokens": 1024,
		"system":     "You are an SRE assistant. Use the available MCP tools to answer observability questions. Always include specific numbers, service names, and trace IDs from the tool results. If a tool returns an error, explain it briefly.",
		"messages":   messages,
		"tools":      tools,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.AnthropicBaseURL+"/v1/messages", strings.NewReader(string(buf)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.cfg.AnthropicAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(raw))
	}

	var pr providerResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decode anthropic: %w", err)
	}
	if pr.Error != nil {
		return nil, fmt.Errorf("anthropic: %s", pr.Error.Message)
	}
	return &pr, nil
}
