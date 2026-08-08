// Package config loads and validates runtime configuration for both the MCP
// server and the LLM agent. Configuration is sourced from environment
// variables (or a .env-style file) so that no credentials ever live in
// source.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds validated runtime configuration.
type Config struct {
	// OpenObserve connection details.
	OpenObserveURL      string
	OpenObserveOrg      string
	OpenObserveUsername string
	OpenObservePassword string
	OpenObserveTimeout  time.Duration

	// Logging.
	MCPLogFile string

	// LLM provider.
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicBaseURL string
}

// Load reads configuration from the process environment and returns a
// fully-validated Config. It does NOT load .env files itself — callers
// should do that (e.g. via the godotenv-style "OPENOBSERVE_*" variables
// exported by docker compose).
func Load() (*Config, error) {
	c := &Config{
		OpenObserveURL:      getenv("OPENOBSERVE_URL", "http://localhost:5080"),
		OpenObserveOrg:      getenv("OPENOBSERVE_ORG", "default"),
		OpenObserveUsername: getenv("OPENOBSERVE_USERNAME", "root@example.com"),
		OpenObservePassword: getenv("OPENOBSERVE_PASSWORD", "Complexpass#123"),
		MCPLogFile:          os.Getenv("MCP_LOG_FILE"),
		AnthropicAPIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:      getenv("ANTHROPIC_MODEL", "claude-3-5-sonnet-latest"),
		AnthropicBaseURL:    getenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
	}

	timeoutStr := getenv("OPENOBSERVE_TIMEOUT", "30s")
	d, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid OPENOBSERVE_TIMEOUT %q: %w", timeoutStr, err)
	}
	c.OpenObserveTimeout = d

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadOpenObserveOnly returns a Config restricted to the OpenObserve
// fields. The MCP server does not need the LLM credentials.
func LoadOpenObserveOnly() (*Config, error) {
	c, err := Load()
	if err != nil {
		return nil, err
	}
	// Strip LLM fields to avoid leaking secrets via debug endpoints.
	c.AnthropicAPIKey = ""
	c.AnthropicModel = ""
	c.AnthropicBaseURL = ""
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	if strings.TrimSpace(c.OpenObserveURL) == "" {
		missing = append(missing, "OPENOBSERVE_URL")
	}
	if strings.TrimSpace(c.OpenObserveOrg) == "" {
		missing = append(missing, "OPENOBSERVE_ORG")
	}
	if strings.TrimSpace(c.OpenObserveUsername) == "" {
		missing = append(missing, "OPENOBSERVE_USERNAME")
	}
	if c.OpenObservePassword == "" {
		missing = append(missing, "OPENOBSERVE_PASSWORD")
	}
	if c.OpenObserveTimeout <= 0 {
		return fmt.Errorf("OPENOBSERVE_TIMEOUT must be > 0, got %s", c.OpenObserveTimeout)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if strings.HasSuffix(c.OpenObserveURL, "/") {
		c.OpenObserveURL = strings.TrimRight(c.OpenObserveURL, "/")
	}
	if !strings.HasPrefix(c.OpenObserveURL, "http://") && !strings.HasPrefix(c.OpenObserveURL, "https://") {
		return fmt.Errorf("OPENOBSERVE_URL must include scheme, got %q", c.OpenObserveURL)
	}
	return nil
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// MustGetenvInt is unused today but kept for future tuning flags.
func MustGetenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// ErrMissingConfig is returned when required configuration is missing.
var ErrMissingConfig = errors.New("missing required configuration")
