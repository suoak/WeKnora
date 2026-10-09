package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Self-contained wire regression also compiles against the preserved old A2
// candidate. Its expected failure there must be a test failure, never a build
// failure; the final candidate must pass it within the complete MCP suite.
func TestBuiltinOAuthWireScope(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.MCPOAuthClient{}, &types.MCPOAuthToken{}))
	repo := repository.NewMCPOAuthRepository(db)
	endpoint := &expiringSessionServer{}
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		if request.Method == "initialize" {
			mu.Lock()
			seen = append(seen, r.Header.Get("Authorization"))
			mu.Unlock()
		}
		endpoint.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "same-principal"}
	service := &types.MCPService{ID: "shared-builtin", IsBuiltin: true, TenantID: 99, Enabled: true, URL: &server.URL, TransportType: types.MCPTransportHTTPStreamable, AuthConfig: &types.MCPAuthConfig{AuthType: types.MCPAuthOAuth}}
	manager := NewMCPManager(repo)
	t.Cleanup(manager.Shutdown)
	clients := make([]MCPClient, 0, 2)
	for _, scope := range []struct {
		tenant uint64
		token  string
	}{{7, "synthetic-a"}, {8, "synthetic-b"}} {
		require.NoError(t, repo.SaveClient(context.Background(), &types.MCPOAuthClient{TenantID: scope.tenant, ServiceID: service.ID, ClientID: "public"}))
		require.NoError(t, repo.SaveTokenForPrincipal(context.Background(), &types.MCPOAuthToken{TenantID: scope.tenant, ServiceID: service.ID, PrincipalType: principal.Type, PrincipalID: principal.ID, UserID: principal.StorageID(), AccessToken: scope.token, TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}))
		ctx := types.WithPrincipal(context.WithValue(context.Background(), types.TenantIDContextKey, scope.tenant), principal)
		c, err := manager.GetOrCreateClient(ctx, service)
		require.NoError(t, err)
		clients = append(clients, c)
	}
	require.EqualValues(t, 2, endpoint.initializations.Load(), "different tenant credentials require independent transport initialization")
	require.NotSame(t, clients[0], clients[1])
	mu.Lock()
	actual := append([]string(nil), seen...)
	mu.Unlock()
	require.Equal(t, []string{"Bearer synthetic-a", "Bearer synthetic-b"}, actual)
}
