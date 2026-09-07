package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
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

func TestMCPPublicURLComesFromApplicationConfig(t *testing.T) {
	h := &TenantHandler{config: &config.Config{MCPPublicURL: "http://10.51.134.114:8082/mcp"}}
	if got := h.mcpPublicURL(); got != "http://10.51.134.114:8082/mcp" {
		t.Fatalf("MCP public URL = %q", got)
	}
	if got := (&TenantHandler{}).mcpPublicURL(); got != "" {
		t.Fatalf("nil-config MCP public URL = %q, want empty", got)
	}
}

func TestMCPAPIKeyResponsesExposeOnlyPublicURL(t *testing.T) {
	publicURL := "http://10.51.134.114:8082/mcp"
	h := &TenantHandler{config: &config.Config{MCPPublicURL: publicURL}}

	for name, response := range map[string]interface{}{
		"list":          h.userMCPCollectionResponse([]gin.H{}),
		"scope-options": h.userMCPCollectionResponse([]gin.H{}),
		"create":        h.userMCPCreateResponse(&types.TenantAPIKey{}, "wk-secret"),
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]interface{}
			if err = json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			container := decoded
			if name == "create" {
				container = decoded["data"].(map[string]interface{})
			}
			if got := container["mcp_public_url"]; got != publicURL {
				t.Fatalf("mcp_public_url = %v", got)
			}
			serialized := string(body)
			for _, secret := range []string{"mcp_server_auth_token", "weknora_api_key", "database_password", "jwt_secret", "system_aes_key"} {
				if strings.Contains(strings.ToLower(serialized), secret) {
					t.Fatalf("response leaked %s", secret)
				}
			}
		})
	}
}
