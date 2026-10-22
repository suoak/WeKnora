package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPortalV1PersonalKeyRecoverableSecretHTTP(t *testing.T) {
	const aesKey = "0123456789abcdef0123456789abcdef"
	t.Setenv("SYSTEM_AES_KEY", aesKey)
	f := fixture.New(t)
	on := true
	g := gin.New()
	tokens := portalM7Management(t, f.DB, g, &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}})
	request := func(method, path, auth string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(auth, "APIKEY:") {
			r.Header.Set("X-API-Key", strings.TrimPrefix(auth, "APIKEY:"))
			r.Header.Set("X-Tenant-ID", "1")
		} else if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/v1/mcp-api-keys", tokens["A"], gin.H{"name": "recoverable", "capabilities": []string{"retrieve", "enterprise_retrieve"}, "tenant_scopes": []gin.H{{"tenant_id": 1, "kb_scope_mode": "all", "knowledge_bases": []any{}}}})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		Data struct {
			ID              uint64 `json:"id"`
			Token           string `json:"token"`
			SecretAvailable bool   `json:"secret_available"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.Data.SecretAvailable)
	token := created.Data.Token
	require.NotEmpty(t, token)
	path := fmt.Sprintf("/api/v1/mcp-api-keys/%d", created.Data.ID)
	var ciphertext string
	require.NoError(t, f.DB.Raw("SELECT api_key FROM tenant_api_keys WHERE id=?", created.Data.ID).Scan(&ciphertext).Error)
	require.True(t, strings.HasPrefix(ciphertext, utils.EncPrefix))
	require.NotContains(t, ciphertext, token)
	for _, p := range []string{"/api/v1/mcp-api-keys", path} {
		w = request("GET", p, tokens["A"], nil)
		require.Equal(t, 200, w.Code)
		require.NotContains(t, w.Body.String(), token)
		require.NotContains(t, w.Body.String(), ciphertext)
	}
	for i := 0; i < 2; i++ {
		w = request("POST", path+"/reveal", tokens["A"], gin.H{})
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), token)
		require.Equal(t, "no-store, private", w.Header().Get("Cache-Control"))
	}
	for _, other := range []string{"", tokens["owner"], tokens["reviewer"], token, "APIKEY:" + token} {
		w = request("POST", path+"/reveal", other, gin.H{})
		require.NotEqual(t, 200, w.Code)
		require.NotContains(t, w.Body.String(), token)
	}
	w = request("POST", "/api/v1/mcp-api-keys/3/reveal", tokens["A"], gin.H{})
	require.Equal(t, 409, w.Code, "hash-only old credential is not invented or migrated")
	oldCiphertext := ciphertext
	w = request("POST", path+"/rotate", tokens["A"], gin.H{})
	require.Equal(t, 200, w.Code, w.Body.String())
	var rotated struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rotated))
	newToken := rotated.Data.Token
	require.NotEqual(t, token, newToken)
	require.NoError(t, f.DB.Raw("SELECT api_key FROM tenant_api_keys WHERE id=?", created.Data.ID).Scan(&ciphertext).Error)
	require.NotEqual(t, oldCiphertext, ciphertext)
	require.NotContains(t, ciphertext, newToken)
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(f.DB))
	_, err := keys.AuthenticateAPIKey(context.Background(), token)
	require.Error(t, err)
	_, err = keys.AuthenticateAPIKey(context.Background(), newToken)
	require.NoError(t, err)
	w = request("POST", path+"/reveal", tokens["A"], gin.H{})
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), newToken)
	require.NotContains(t, w.Body.String(), token)
	t.Setenv("SYSTEM_AES_KEY", "")
	w = request("POST", path+"/reveal", tokens["A"], gin.H{})
	require.NotEqual(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), newToken)
	_, err = keys.AuthenticateAPIKey(context.Background(), newToken)
	require.NoError(t, err, "hash authentication does not require secret decryption")
	t.Setenv("SYSTEM_AES_KEY", aesKey)
	require.NoError(t, f.DB.Exec("UPDATE users SET is_active=false WHERE id='A'").Error)
	w = request("POST", path+"/reveal", tokens["A"], gin.H{})
	require.NotEqual(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), newToken)
	require.NoError(t, f.DB.Exec("UPDATE users SET is_active=true WHERE id='A'").Error)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET expires_at=? WHERE id=?", time.Now().Add(-time.Hour), created.Data.ID).Error)
	w = request("POST", path+"/reveal", tokens["A"], gin.H{})
	require.Equal(t, 403, w.Code)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET expires_at=NULL WHERE id=?", created.Data.ID).Error)
	w = request("DELETE", path, tokens["A"], nil)
	require.Equal(t, 200, w.Code)
	w = request("POST", path+"/reveal", tokens["A"], gin.H{})
	require.Equal(t, 403, w.Code)
	require.NotContains(t, w.Body.String(), newToken)
}

func TestPortalV1PersonalKeyMissingAESNeverStoresPlaintext(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "")
	f := fixture.New(t)
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(f.DB))
	key, err := keys.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{ScopeType: "user_mcp", OwnerUserID: "A", Name: "no encryption configured", Capabilities: []string{"retrieve"}, TenantScopes: []types.APIKeyTenantScope{{TenantID: 1, KBScopeMode: "all"}}})
	require.NoError(t, err)
	var stored string
	require.NoError(t, f.DB.Raw("SELECT api_key FROM tenant_api_keys WHERE id=?", key.APIKey.ID).Scan(&stored).Error)
	require.Empty(t, stored)
}
