package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

type fakeMCPServiceRepo struct {
	interfaces.MCPServiceRepository
	service *types.MCPService
}

func (r fakeMCPServiceRepo) GetByID(_ context.Context, tenant uint64, id string) (*types.MCPService, error) {
	if r.service != nil && r.service.ID == id && r.service.TenantID == tenant {
		return r.service, nil
	}
	return nil, nil
}

// An authorization server may register a confidential client and return a
// client secret along with its ID. The code exchange runs in the callback
// request, with a handler rebuilt from the stored client, so the secret has
// to be stored too or the token request goes out without it.
func TestOAuthManagerKeepsTheRegisteredClientSecret(t *testing.T) {
	for _, secret := range []string{"registered-secret", ""} {
		t.Run(map[bool]string{true: "public", false: "confidential"}[secret == ""], func(t *testing.T) {
			t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("SSRF_WHITELIST", "127.0.0.1")
			secutils.ResetSSRFWhitelistForTest()
			t.Cleanup(secutils.ResetSSRFWhitelistForTest)

			var tokenRequestSecret atomic.Value
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/oauth-authorization-server":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"issuer":                 server.URL,
						"authorization_endpoint": server.URL + "/authorize",
						"token_endpoint":         server.URL + "/token",
						"registration_endpoint":  server.URL + "/register",
					})
				case "/register":
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"client_id":     "registered-client",
						"client_secret": secret,
					})
				case "/token":
					_ = r.ParseForm()
					requestSecret := r.PostForm.Get("client_secret")
					tokenRequestSecret.Store(requestSecret)
					if requestSecret != secret {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client"})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "access",
						"token_type":    "Bearer",
						"expires_in":    3600,
						"refresh_token": "refresh",
					})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			serviceURL := server.URL + "/mcp"
			service := &types.MCPService{
				ID:            "svc-1",
				TenantID:      7,
				TransportType: types.MCPTransportHTTPStreamable,
				URL:           &serviceURL,
				AuthConfig: &types.MCPAuthConfig{
					AuthType:              types.MCPAuthOAuth,
					AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server",
				},
			}
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			require.NoError(t, db.AutoMigrate(&types.MCPOAuthClient{}, &types.MCPOAuthToken{}))
			repo := repository.NewMCPOAuthRepository(db)
			manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
			principal := types.Principal{Type: types.PrincipalWebUser, ID: "42"}
			ctx := context.Background()

			_, state, err := manager.StartAuthorization(ctx, service, 7, principal, "http://localhost:8080/callback", "/")
			require.NoError(t, err)
			stored, err := repo.GetClient(ctx, 7, "svc-1")
			require.NoError(t, err)
			require.NotNil(t, stored)
			require.Equal(t, "registered-client", stored.ClientID)
			require.Equal(t, secret, stored.ClientSecret)
			var raw string
			require.NoError(t, db.Raw("SELECT client_secret FROM mcp_oauth_clients WHERE tenant_id = ? AND service_id = ?", 7, "svc-1").Scan(&raw).Error)
			if secret != "" {
				require.Contains(t, raw, secutils.EncPrefix)
				require.NotContains(t, raw, secret)
			} else {
				require.Empty(t, raw)
			}
			encoded, err := json.Marshal(stored)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "client_secret")
			other, err := repo.GetClient(ctx, 8, "svc-1")
			require.NoError(t, err)
			require.Nil(t, other)
			require.NoError(t, repo.SaveClient(ctx, &types.MCPOAuthClient{TenantID: 8, ServiceID: "svc-1", ClientID: "other-tenant", ClientSecret: "other-secret"}))
			stored, err = repo.GetClient(ctx, 7, "svc-1")
			require.NoError(t, err)
			require.Equal(t, secret, stored.ClientSecret)
			other, err = repo.GetClient(ctx, 8, "svc-1")
			require.NoError(t, err)
			require.Equal(t, "other-secret", other.ClientSecret)
			other, err = repo.GetClient(ctx, 7, "other-service")
			require.NoError(t, err)
			require.Nil(t, other)

			_, _, err = manager.CompleteAuthorization(ctx, state, "code")
			require.NoError(t, err)
			require.Equal(t, secret, tokenRequestSecret.Load())

			token, err := repo.GetTokenForPrincipal(ctx, 7, principal, "svc-1")
			require.NoError(t, err)
			require.NotNil(t, token)
			otherToken, err := repo.GetTokenForPrincipal(ctx, 8, principal, "svc-1")
			require.NoError(t, err)
			require.Nil(t, otherToken)
			otherToken, err = repo.GetTokenForPrincipal(ctx, 7, types.Principal{Type: types.PrincipalWebUser, ID: "other"}, "svc-1")
			require.NoError(t, err)
			require.Nil(t, otherToken)
			h, err := manager.newHandler(ctx, service, 7, principal, "http://localhost:8080/callback")
			require.NoError(t, err)
			refreshed, err := h.RefreshToken(ctx, "refresh")
			require.NoError(t, err)
			require.NotNil(t, refreshed)
			require.Equal(t, secret, tokenRequestSecret.Load())
			token, err = repo.GetTokenForPrincipal(ctx, 7, principal, "svc-1")
			require.NoError(t, err)
			require.NotNil(t, token)
			// Exercise an upsert using the real hooks, not only an initial insert.
			require.NoError(t, repo.SaveClient(ctx, &types.MCPOAuthClient{TenantID: 7, ServiceID: "svc-1", ClientID: "registered-client", ClientSecret: secret}))
			stored, err = repo.GetClient(ctx, 7, "svc-1")
			require.NoError(t, err)
			require.Equal(t, secret, stored.ClientSecret)
			if secret != "" {
				t.Setenv("SYSTEM_AES_KEY", "fedcba9876543210fedcba9876543210")
				stored, err = repo.GetClient(ctx, 7, "svc-1")
				require.NoError(t, err)
				require.Empty(t, stored.ClientSecret, "bad key must not expose ciphertext as a credential")
			}
		})
	}
}

type failingClientRepo struct {
	interfaces.MCPOAuthRepository
	failRead bool
}

func (r failingClientRepo) GetClient(ctx context.Context, tenant uint64, service string) (*types.MCPOAuthClient, error) {
	if r.failRead {
		return nil, errors.New("database parameters contain registered-secret")
	}
	return r.MCPOAuthRepository.GetClient(ctx, tenant, service)
}
func (r failingClientRepo) SaveClient(context.Context, *types.MCPOAuthClient) error {
	return errors.New("database parameters contain registered-secret")
}

func TestOAuthManagerClientStorageFailureIsNotAuthorizationSuccess(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client", "client_secret": "registered-secret"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "registration_endpoint": server.URL + "/register", "token_endpoint": server.URL + "/token"})
	}))
	defer server.Close()
	u := server.URL + "/mcp"
	service := &types.MCPService{ID: "svc", TenantID: 7, URL: &u, AuthConfig: &types.MCPAuthConfig{AuthType: types.MCPAuthOAuth, AuthServerMetadataURL: server.URL + "/.well-known/oauth-authorization-server"}}
	for _, readFail := range []bool{false, true} {
		manager := NewOAuthManager(failingClientRepo{MCPOAuthRepository: newFakeOAuthRepo(), failRead: readFail}, nil, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		authURL, state, err := manager.StartAuthorization(ctx, service, 7, types.Principal{Type: types.PrincipalWebUser, ID: "user"}, "http://localhost/callback", "/")
		cancel()
		require.Error(t, err)
		require.Empty(t, authURL)
		require.Empty(t, state)
		require.NotContains(t, err.Error(), "registered-secret")
	}
}
