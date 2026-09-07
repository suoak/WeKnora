package config

import "testing"

func TestApplyMCPPublicURLEnv(t *testing.T) {
	// Release mode must not discard an explicitly configured HTTP endpoint;
	// private/offline installations commonly terminate TLS elsewhere or do not
	// expose this network at all.
	t.Setenv("GIN_MODE", "release")

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "internal HTTP release deployment", value: "http://10.51.134.114:8082/mcp", want: "http://10.51.134.114:8082/mcp"},
		{name: "HTTPS trailing slash", value: " https://knowledge.example.com/mcp/ ", want: "https://knowledge.example.com/mcp"},
		{name: "unconfigured", value: "", want: ""},
		{name: "invalid scheme", value: "file:///tmp/mcp", want: ""},
		{name: "missing host", value: "http:///mcp", want: ""},
		{name: "reject credentials", value: "https://user:secret@example.com/mcp", want: ""},
		{name: "reject query secrets", value: "https://example.com/mcp?token=secret", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MCP_PUBLIC_URL", tt.value)
			cfg := &Config{MCPPublicURL: "must-be-overwritten"}
			applyMCPPublicURLEnv(cfg)
			if cfg.MCPPublicURL != tt.want {
				t.Fatalf("MCPPublicURL = %q, want %q", cfg.MCPPublicURL, tt.want)
			}
		})
	}
}
