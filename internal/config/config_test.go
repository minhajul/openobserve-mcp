package config

import (
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"OPENOBSERVE_URL", "OPENOBSERVE_ORG", "OPENOBSERVE_USERNAME", "OPENOBSERVE_PASSWORD", "OPENOBSERVE_TIMEOUT", "MCP_LOG_FILE", "MCP_LOG_LEVEL"} {
		t.Setenv(k, kv[k])
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.OpenObserveURL != "http://localhost:5080" || c.OpenObserveOrg != "default" || c.OpenObserveTimeout != 30*time.Second || c.MCPLogLevel != "info" {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestLoadTrimsTrailingSlash(t *testing.T) {
	setEnv(t, map[string]string{"OPENOBSERVE_URL": "https://oo.example.com//"})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.OpenObserveURL != "https://oo.example.com" {
		t.Errorf("URL = %q", c.OpenObserveURL)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"bad timeout":      {map[string]string{"OPENOBSERVE_TIMEOUT": "soon"}, "OPENOBSERVE_TIMEOUT"},
		"negative timeout": {map[string]string{"OPENOBSERVE_TIMEOUT": "-1s"}, "OPENOBSERVE_TIMEOUT"},
		"no scheme":        {map[string]string{"OPENOBSERVE_URL": "localhost:5080"}, "scheme"},
		"bad log level":    {map[string]string{"MCP_LOG_LEVEL": "verbose"}, "MCP_LOG_LEVEL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, tc.env)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want mention of %q", err, tc.want)
			}
		})
	}
}
