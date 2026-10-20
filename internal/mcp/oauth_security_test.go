package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	applog "github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

const syntheticOAuthSensitive = "synthetic-client-secret synthetic-access-token " +
	"synthetic-refresh-token postgres://synthetic-password"

func securityOAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	db, err := gorm.Open(
		sqlite.Open(":memory:"),
		&gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)},
	)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.MCPOAuthClient{}, &types.MCPOAuthToken{}))
	return db
}

func TestOAuthAuthorizationFailClosedStateAndRecovery(t *testing.T) {
	for _, mode := range []string{"missing-key", "invalid-key", "sql-error", "public"} {
		t.Run(mode, func(t *testing.T) {
			db := securityOAuthDB(t)
			repo := repository.NewMCPOAuthRepository(db)
			utils.SetSSRFWhitelistFromRaw("127.0.0.1")
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			secret := "synthetic-client-secret"
			if mode == "public" {
				secret = ""
			}
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/register" {
					w.WriteHeader(201)
					_ = json.NewEncoder(w).
						Encode(map[string]any{"client_id": "client", "client_secret": secret})
					return
				}
				_ = json.NewEncoder(w).
					Encode(map[string]any{
						"issuer": server.URL,
						"registration_endpoint": server.URL +
							"/register",
						"authorization_endpoint": server.URL +
							"/authorize",
						"token_endpoint": server.URL +
							"/token",
					})
			}))
			t.Cleanup(server.Close)
			u := server.URL + "/mcp"
			service := &types.MCPService{
				ID:       "svc",
				TenantID: 7,
				URL:      &u,
				AuthConfig: &types.MCPAuthConfig{
					AuthType:              types.MCPAuthOAuth,
					AuthServerMetadataURL: server.URL + "/metadata",
				},
			}
			manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
			principal := types.Principal{Type: types.PrincipalWebUser, ID: "user"}
			switch mode {
			case "missing-key", "public":
				t.Setenv("SYSTEM_AES_KEY", "")
			case "invalid-key":
				t.Setenv("SYSTEM_AES_KEY", "short")
			case "sql-error":
				require.NoError(
					t,
					db.Exec(
						"CREATE TRIGGER reject_oauth_client BEFORE INSERT ON mcp_oauth_clients "+
							"BEGIN SELECT RAISE(ABORT, 'synthetic-client-secret SQL parameters'); "+
							"END",
					).Error,
				)
			}
			url, state, err := manager.StartAuthorization(
				context.Background(),
				service,
				7,
				principal,
				"http://localhost/callback",
				"/",
			)
			var count int64
			require.NoError(t, db.Model(&types.MCPOAuthClient{}).Count(&count).Error)
			if mode == "public" {
				require.NoError(t, err)
				require.NotEmpty(t, url)
				require.NotEmpty(t, state)
				require.EqualValues(t, 1, count)
				return
			}
			require.Error(t, err)
			require.Empty(t, url)
			require.Empty(t, state)
			require.Zero(t, count)
			manager.states.mu.Lock()
			states, attempts := len(manager.states.mem), len(manager.states.attempts)
			manager.states.mu.Unlock()
			require.Zero(t, states)
			require.Zero(t, attempts)
			require.NotContains(t, err.Error(), "synthetic-client-secret")
			if mode == "sql-error" {
				require.NoError(t, db.Exec("DROP TRIGGER reject_oauth_client").Error)
			}
			t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
			url, state, err = manager.StartAuthorization(
				context.Background(),
				service,
				7,
				principal,
				"http://localhost/callback",
				"/",
			)
			require.NoError(t, err)
			require.NotEmpty(t, url)
			require.NotEmpty(t, state)
			row, err := repo.GetClient(context.Background(), 7, "svc")
			require.NoError(t, err)
			require.Equal(t, secret, row.ClientSecret)
		})
	}
}

func TestOAuthMaliciousProviderErrorsAndLogsAreRedacted(t *testing.T) {
	for _, phase := range []string{"registration", "exchange", "discovery"} {
		t.Run(phase, func(t *testing.T) {
			db := securityOAuthDB(t)
			repo := repository.NewMCPOAuthRepository(db)
			utils.SetSSRFWhitelistFromRaw("127.0.0.1")
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			var fail atomic.Bool
			fail.Store(phase != "exchange")
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if fail.Load() &&
					((phase == "registration" &&
						r.URL.Path == "/register") ||
						(phase == "exchange" &&
							r.URL.Path == "/token") ||
						phase == "discovery") {
					w.WriteHeader(400)
					_ = json.NewEncoder(w).
						Encode(map[string]any{"error": "invalid_client", "error_description": syntheticOAuthSensitive})
					return
				}
				if r.URL.Path == "/register" {
					w.WriteHeader(201)
					_ = json.NewEncoder(w).
						Encode(map[string]any{"client_id": "client", "client_secret": "synthetic-client-secret"})
					return
				}
				_ = json.NewEncoder(w).
					Encode(map[string]any{
						"issuer": server.URL,
						"registration_endpoint": server.URL +
							"/register",
						"authorization_endpoint": server.URL +
							"/authorize",
						"token_endpoint": server.URL +
							"/token",
					})
			}))
			t.Cleanup(server.Close)
			u := server.URL + "/mcp"
			service := &types.MCPService{
				ID:       "svc",
				TenantID: 7,
				URL:      &u,
				AuthConfig: &types.MCPAuthConfig{
					AuthType:              types.MCPAuthOAuth,
					AuthServerMetadataURL: server.URL + "/metadata",
				},
			}
			var captured bytes.Buffer
			log := logrus.New()
			log.SetOutput(&captured)
			ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
			manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
			_, state, err := manager.StartAuthorization(
				ctx,
				service,
				7,
				types.Principal{Type: types.PrincipalWebUser, ID: "user"},
				"http://localhost/callback",
				"/",
			)
			if phase == "exchange" {
				require.NoError(t, err)
				fail.Store(true)
				_, _, err = manager.CompleteAuthorization(ctx, state, "synthetic-code")
			}
			require.Error(t, err)
			applog.GetLogger(ctx).Errorf("OAuth failure: %v", err)
			require.NotEmpty(t, captured.String())
			for _, s := range []string{
				"synthetic-client-secret",

				"synthetic-access-token",

				"synthetic-refresh-token",

				"synthetic-password",
			} {

				require.NotContains(t, err.Error(), s)
				require.NotContains(t, captured.String(), s)
				require.NotContains(t, SafeOAuthErrorMessage(err), s)

			}
		})
	}
}

func TestOAuthRefreshRedactionPreservesClassification(t *testing.T) {
	runtime, repo, requests, closeServer := newOAuthLifecycleFixture(
		t,
		503,
		map[string]any{"error": "temporarily_unavailable", "error_description": syntheticOAuthSensitive},
	)
	defer closeServer()
	err := runtime.ensureFresh(context.Background(), false, nil)
	var temporary *OAuthRefreshTemporaryError
	require.ErrorAs(t, err, &temporary)
	require.EqualValues(t, 1, requests.Load())
	require.Contains(t, err.Error(), "temporarily_unavailable")
	var captured bytes.Buffer
	log := logrus.New()
	log.SetOutput(&captured)
	ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
	applog.GetLogger(ctx).Errorf("refresh: %v", err)
	for _, s := range []string{
		"synthetic-client-secret",

		"synthetic-access-token",

		"synthetic-refresh-token",

		"synthetic-password",
	} {
		require.NotContains(t, captured.String(), s)
	}
	row, err := repo.GetTokenForPrincipal(
		ctx,
		7,
		types.Principal{Type: types.PrincipalWebUser, ID: "user-1"},
		"svc-1",
	)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", row.RefreshToken)
	require.ErrorIs(t, safeOAuthError("read", context.DeadlineExceeded), context.DeadlineExceeded)
	require.NotContains(t, SafeOAuthErrorMessage(errors.New(syntheticOAuthSensitive)), "synthetic")
}
