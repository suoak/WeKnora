package handler

import (
	"testing"
	"time"
)

func TestUserMCPExpiryDefaultsToNinetyDays(t *testing.T) {
	before := time.Now().UTC().Add(90 * 24 * time.Hour)
	expires, err := userMCPExpiry(userMCPKeyRequest{})
	after := time.Now().UTC().Add(90 * 24 * time.Hour)
	if err != nil || expires == nil || expires.Before(before) || expires.After(after) {
		t.Fatalf("expiry = %v, err = %v; want approximately 90 days", expires, err)
	}
}

func TestUserMCPExpiryAllowsNeverAndRejectsPast(t *testing.T) {
	expires, err := userMCPExpiry(userMCPKeyRequest{NeverExpires: true})
	if err != nil || expires != nil {
		t.Fatalf("never expiry = %v, err = %v", expires, err)
	}
	past := time.Now().Add(-time.Hour).Unix()
	if _, err = userMCPExpiry(userMCPKeyRequest{ExpiresAt: &past}); err == nil {
		t.Fatal("past expiry was accepted")
	}
}

func TestMCPPublicURLRequiresHTTPSInRelease(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("MCP_PUBLIC_URL", "http://knowledge.example.com/mcp")
	if got := mcpPublicURL(); got != "" {
		t.Fatalf("release HTTP URL = %q, want disabled", got)
	}
	t.Setenv("MCP_PUBLIC_URL", "https://knowledge.example.com/mcp/")
	if got := mcpPublicURL(); got != "https://knowledge.example.com/mcp" {
		t.Fatalf("HTTPS URL = %q", got)
	}
}
