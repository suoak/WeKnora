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

func TestUserMCPExpiryRejectsConflictingPolicy(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	if _, err := userMCPExpiry(userMCPKeyRequest{NeverExpires: true, ExpiresAt: &future}); err == nil {
		t.Fatal("create accepted never_expires and expires_at_unix together")
	}
}

func TestUserMCPExpiryChangeContract(t *testing.T) {
	truth := true
	falsity := false
	futureUnix := time.Now().Add(24 * time.Hour).Unix()
	pastUnix := time.Now().Add(-time.Hour).Unix()

	specified, expiry, err := userMCPExpiryChange(nil, nil)
	if err != nil || specified || expiry != nil {
		t.Fatalf("omitted expiry = specified:%v expiry:%v err:%v; want unchanged", specified, expiry, err)
	}
	specified, expiry, err = userMCPExpiryChange(nil, &truth)
	if err != nil || !specified || expiry != nil {
		t.Fatalf("never expiry = specified:%v expiry:%v err:%v", specified, expiry, err)
	}
	specified, expiry, err = userMCPExpiryChange(&futureUnix, nil)
	if err != nil || !specified || expiry == nil || expiry.Unix() != futureUnix {
		t.Fatalf("dated expiry = specified:%v expiry:%v err:%v", specified, expiry, err)
	}
	if _, _, err = userMCPExpiryChange(&futureUnix, &truth); err == nil {
		t.Fatal("PATCH accepted mutually exclusive expiry fields")
	}
	if _, _, err = userMCPExpiryChange(&pastUnix, nil); err == nil {
		t.Fatal("PATCH accepted past expiry")
	}
	if _, _, err = userMCPExpiryChange(nil, &falsity); err == nil {
		t.Fatal("PATCH accepted never_expires=false without an expiry")
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

func TestUserMCPCredentialResponsesNeverExposeStoredSecret(t *testing.T) {
	now := time.Now().UTC()
	key := &types.TenantAPIKey{
		ID: 42, ScopeType: types.APIKeyScopeUserMCP, Name: "WorkBuddy",
		ClientType: types.MCPClientWorkBuddy, APIKey: "synthetic-stored-secret-never-return",
		TokenHint: "••••92AF", Capabilities: types.StringArray{"retrieve", "chat"}, CreatedAt: now,
	}
	for name, response := range map[string]any{
		"list-detail-patch": userMCPResponse(key),
		"create-rotate":     (&TenantHandler{}).userMCPCreateResponse(key, "synthetic-one-time-token"),
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			serialized := string(body)
			if strings.Contains(serialized, key.APIKey) || strings.Contains(serialized, `"api_key"`) {
				t.Fatalf("response exposed stored api_key: %s", serialized)
			}
			if name == "create-rotate" && !strings.Contains(serialized, "synthetic-one-time-token") {
				t.Fatalf("one-time response omitted generated token: %s", serialized)
			}
		})
	}
}

func TestUserMCPAuditDetailsContainHintButNoSecret(t *testing.T) {
	key := &types.TenantAPIKey{
		ID: 7, Name: "Cursor", ClientType: types.MCPClientCursor,
		APIKey: "synthetic-audit-secret-never-log", TokenHint: "••••ABCD",
		Capabilities: types.StringArray{"retrieve"},
	}
	details := userMCPAuditDetails(key, []types.APIKeyTenantScope{{
		TenantID: 11, KBScopeMode: types.APIKeyKBScopeSelected,
		KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{KnowledgeBaseID: "kb-1"}},
	}})
	serialized := string(details)
	if strings.Contains(serialized, key.APIKey) || !strings.Contains(serialized, key.TokenHint) {
		t.Fatalf("unsafe audit details: %s", serialized)
	}
}
