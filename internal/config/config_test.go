package config

import (
	"os"
	"testing"
	"time"
)

func clearOOEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENOBSERVE_URL", "OPENOBSERVE_ORG", "OPENOBSERVE_USERNAME",
		"OPENOBSERVE_PASSWORD", "OPENOBSERVE_TIMEOUT", "ANTHROPIC_API_KEY",
	} {
		os.Unsetenv(k)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearOOEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.OpenObserveURL != "http://localhost:5080" {
		t.Errorf("URL default = %q", c.OpenObserveURL)
	}
	if c.OpenObserveOrg != "default" {
		t.Errorf("Org default = %q", c.OpenObserveOrg)
	}
	if c.OpenObserveTimeout != 30*time.Second {
		t.Errorf("Timeout default = %s", c.OpenObserveTimeout)
	}
	if c.OpenObservePassword == "" {
		t.Errorf("Password default is empty")
	}
}

func TestLoadTrailingSlashTrimmed(t *testing.T) {
	clearOOEnv(t)
	os.Setenv("OPENOBSERVE_URL", "http://localhost:5080/")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.OpenObserveURL != "http://localhost:5080" {
		t.Errorf("expected trailing slash trimmed, got %q", c.OpenObserveURL)
	}
}

func TestLoadInvalidURL(t *testing.T) {
	clearOOEnv(t)
	os.Setenv("OPENOBSERVE_URL", "localhost:5080")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for url without scheme")
	}
}

func TestLoadInvalidTimeout(t *testing.T) {
	clearOOEnv(t)
	os.Setenv("OPENOBSERVE_TIMEOUT", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for invalid duration")
	}
}

func TestLoadOpenObserveOnlyStripsLLM(t *testing.T) {
	clearOOEnv(t)
	os.Setenv("ANTHROPIC_API_KEY", "sk-test")
	c, err := LoadOpenObserveOnly()
	if err != nil {
		t.Fatalf("LoadOpenObserveOnly: %v", err)
	}
	if c.AnthropicAPIKey != "" {
		t.Errorf("expected anthropic api key stripped, got %q", c.AnthropicAPIKey)
	}
}

func TestValidateRejectsEmptyValues(t *testing.T) {
	c := &Config{
		OpenObserveURL:      "http://x",
		OpenObserveOrg:      "default",
		OpenObserveUsername: "u",
		OpenObservePassword: "p",
		OpenObserveTimeout:  5 * time.Second,
	}
	if err := c.validate(); err != nil {
		t.Errorf("baseline invalid: %v", err)
	}
	c.OpenObserveURL = ""
	if err := c.validate(); err == nil {
		t.Errorf("expected error for empty URL")
	}
	c.OpenObserveURL = "ftp://x"
	if err := c.validate(); err == nil {
		t.Errorf("expected error for bad scheme")
	}
	c.OpenObserveURL = "http://x"
	c.OpenObserveTimeout = 0
	if err := c.validate(); err == nil {
		t.Errorf("expected error for zero timeout")
	}
}
