package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	OpenObserveURL      string
	OpenObserveOrg      string
	OpenObserveUsername string
	OpenObservePassword string
	OpenObserveTimeout  time.Duration

	MCPLogFile  string
	MCPLogLevel string
}

func Load() (*Config, error) {
	c := &Config{
		OpenObserveURL:      getenv("OPENOBSERVE_URL", "http://localhost:5080"),
		OpenObserveOrg:      getenv("OPENOBSERVE_ORG", "default"),
		OpenObserveUsername: getenv("OPENOBSERVE_USERNAME", "root@example.com"),
		OpenObservePassword: getenv("OPENOBSERVE_PASSWORD", "Complexpass#123"),
		MCPLogFile:          os.Getenv("MCP_LOG_FILE"),
		MCPLogLevel:         getenv("MCP_LOG_LEVEL", "info"),
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
	if c.MCPLogLevel != "info" && c.MCPLogLevel != "debug" {
		return fmt.Errorf("MCP_LOG_LEVEL must be \"info\" or \"debug\", got %q", c.MCPLogLevel)
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
