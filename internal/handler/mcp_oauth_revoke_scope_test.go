package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/mcp"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	msrv "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOAuthRevokeSuccessClearsOnlyAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	backend := msrv.NewStreamableHTTPServer(msrv.NewMCPServer("isolated-revoke", "1"), msrv.WithStateLess(true))
	var mu sync.Mutex
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")]++
		mu.Unlock()
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	service := &types.MCPService{ID: "builtin", TenantID: 99, IsBuiltin: true, Enabled: true, URL: &server.URL, TransportType: types.MCPTransportHTTPStreamable, AuthConfig: &types.MCPAuthConfig{AuthType: types.MCPAuthOAuth}}
	manager := mcp.NewMCPManager(repo)
	t.Cleanup(manager.Shutdown)
	alice := types.Principal{Type: types.PrincipalWebUser, ID: "same-user"}
	bob := types.Principal{Type: types.PrincipalWebUser, ID: "other-user"}
	clients := make([]mcp.MCPClient, 0, 3)
	contexts := make([]context.Context, 0, 3)
	for _, scope := range []struct {
		tenant    uint64
		principal types.Principal
		token     string
	}{
		{7, alice, "synthetic-revoke-a"}, {8, alice, "synthetic-revoke-b"}, {7, bob, "synthetic-revoke-other"},
	} {
		require.NoError(t, repo.SaveClient(context.Background(), &types.MCPOAuthClient{TenantID: scope.tenant, ServiceID: service.ID, ClientID: "public"}))
		require.NoError(t, repo.SaveTokenForPrincipal(context.Background(), &types.MCPOAuthToken{TenantID: scope.tenant, ServiceID: service.ID, PrincipalType: scope.principal.Type, PrincipalID: scope.principal.ID, UserID: scope.principal.StorageID(), AccessToken: scope.token, TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}))
		ctx := types.WithPrincipal(types.WithExecutionTenant(context.Background(), scope.tenant), scope.principal)
		c, err := manager.GetOrCreateClient(ctx, service)
		require.NoError(t, err)
		clients = append(clients, c)
		contexts = append(contexts, ctx)
	}
	mu.Lock()
	actual := make(map[string]int, len(seen))
	for header, count := range seen {
		actual[header] = count
	}
	mu.Unlock()
	for _, header := range []string{"Bearer synthetic-revoke-a", "Bearer synthetic-revoke-b", "Bearer synthetic-revoke-other"} {
		require.Positive(t, actual[header], "all scopes must establish real authorized transport")
	}
	oauth := mcp.NewOAuthManager(repo, nil, nil)
	h := NewMCPOAuthHandler(oauth, manager, securityOAuthService{service: service}, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)); c.Next() })
	router.DELETE("/revoke/:id", h.Revoke)
	request := httptest.NewRequest("DELETE", "/revoke/builtin?tenant_id=8&principal_id=other-user", nil).WithContext(contexts[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, response.Body.String())
	require.False(t, clients[0].IsConnected())
	require.True(t, clients[1].IsConnected())
	require.True(t, clients[2].IsConnected())
	row, err := repo.GetTokenForPrincipal(context.Background(), 7, alice, service.ID)
	require.NoError(t, err)
	require.Nil(t, row)
	for _, scope := range []struct {
		tenant    uint64
		principal types.Principal
		index     int
		token     string
	}{
		{8, alice, 1, "synthetic-revoke-b"}, {7, bob, 2, "synthetic-revoke-other"},
	} {
		row, err := repo.GetTokenForPrincipal(context.Background(), scope.tenant, scope.principal, service.ID)
		require.NoError(t, err)
		require.Equal(t, scope.token, row.AccessToken)
		c, err := manager.GetOrCreateClient(contexts[scope.index], service)
		require.NoError(t, err)
		require.Same(t, clients[scope.index], c)
	}
	_, err = manager.GetOrCreateClient(contexts[0], service)
	require.Error(t, err, "revoked scope cannot reuse the old authorized connection")
}
