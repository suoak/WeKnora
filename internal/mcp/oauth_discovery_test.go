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
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	applog "github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

var discoveryBadCases = []string{
	"http-400", "http-401", "http-403", "http-500", "empty", "null", "malformed",
	"empty-object", "missing-authorization", "missing-token", "missing-registration",
	"unsafe-endpoint", "interrupted", "deadline", "cancel",
}

func TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects(t *testing.T) {
	for _, mode := range discoveryBadCases {
		t.Run(mode, func(t *testing.T) {
			db := securityOAuthDB(t)
			repo := repository.NewMCPOAuthRepository(db)
			utils.SetSSRFWhitelistFromRaw("127.0.0.1")
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			var discoveries, registrations atomic.Int32
			entered := make(chan struct{}, 1)
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/register" {
					registrations.Add(1)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write(
						[]byte("{\"client_id\":\"invalid-client\",\"client_secret\":\"synthetic-client" +
							"-secret\"}"),
					)
					return
				}
				discoveries.Add(1)
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "http-400", "http-401", "http-403", "http-500":
					status := map[string]int{"http-400": 400, "http-401": 401, "http-403": 403, "http-500": 500}[mode]
					w.WriteHeader(status)
					_ = json.NewEncoder(w).
						Encode(map[string]string{
							"error":             "invalid_client",
							"error_description": syntheticOAuthSensitive,
						})
				case "empty":
				case "null":
					_, _ = w.Write([]byte("null"))
				case "malformed":
					_, _ = w.Write([]byte(`{"secret":"synthetic-client-secret",`))
				case "empty-object":
					_, _ = w.Write([]byte(`{}`))
				case "interrupted":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				case "deadline", "cancel":
					entered <- struct{}{}
					<-r.Context().Done()
				default:
					metadata := map[string]string{
						"issuer":                 server.URL,
						"authorization_endpoint": server.URL + "/authorize",
						"token_endpoint":         server.URL + "/token",
						"registration_endpoint":  server.URL + "/register",
					}
					switch mode {
					case "missing-authorization":
						delete(metadata, "authorization_endpoint")
					case "missing-token":
						delete(metadata, "token_endpoint")
					case "missing-registration":
						delete(metadata, "registration_endpoint")
					case "unsafe-endpoint":
						metadata["token_endpoint"] = "file:///synthetic-client-secret"
					}
					_ = json.NewEncoder(w).Encode(metadata)
				}
			}))
			t.Cleanup(server.Close)
			u := server.URL + "/mcp"
			service := &types.MCPService{
				ID:       "discovery",
				TenantID: 7,
				URL:      &u,
				AuthConfig: &types.MCPAuthConfig{
					AuthType:              types.MCPAuthOAuth,
					AuthServerMetadataURL: server.URL + "/metadata",
				},
			}
			manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
			var captured bytes.Buffer
			log := logrus.New()
			log.SetOutput(&captured)
			ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if mode == "cancel" {
				// Signal cancellation only after the request is in flight.
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			url, state, err := manager.StartAuthorization(
				ctx,
				service,
				7,
				types.Principal{Type: types.PrincipalWebUser, ID: "user"},
				"http://localhost/callback",
				"/",
			)
			// A panic fails the test naturally: no recover or swallowed errors.
			require.Error(t, err)
			var safe *OAuthSafeError
			require.ErrorAs(t, err, &safe)
			require.Equal(t, "mcp_oauth_discovery_failed", safe.Code)
			require.False(t, errors.Is(err, transport.ErrSessionTerminated))
			if mode == "deadline" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, url)
			require.Empty(t, state)
			require.EqualValues(t, 1, discoveries.Load(), "preflight and consumer must share SDK cache")
			require.Zero(t, registrations.Load(), "invalid discovery must never register")
			var count int64
			require.NoError(t, db.Model(&types.MCPOAuthClient{}).Count(&count).Error)
			require.Zero(t, count)
			manager.states.mu.Lock()
			states, attempts := len(manager.states.mem), len(manager.states.attempts)
			manager.states.mu.Unlock()
			require.Zero(t, states)
			require.Zero(t, attempts)
			applog.GetLogger(ctx).Errorf("OAuth discovery: %v", err)
			for _, secret := range []string{
				"synthetic-client-secret",
				"synthetic-access-token",
				"synthetic-refresh-token",
				"synthetic-password",
			} {
				require.NotContains(t, err.Error(), secret)
				require.NotContains(t, SafeOAuthErrorMessage(err), secret)
				require.NotContains(t, captured.String(), secret)
			}
		})
	}
}

func TestOAuthDiscoveryUsesConsumerCacheAndGuardsRebuiltHandlers(t *testing.T) {
	db := securityOAuthDB(t)
	repo := repository.NewMCPOAuthRepository(db)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var discoveries, registrations, exchanges atomic.Int32
	var invalid atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/metadata":
			discoveries.Add(1)
			if invalid.Load() {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(syntheticOAuthSensitive))
				return
			}
			_ = json.NewEncoder(w).
				Encode(map[string]string{
					"issuer": server.URL,
					"authorization_endpoint": server.URL +
						"/authorize",
					"token_endpoint": server.URL +
						"/token",
					"registration_endpoint": server.URL +
						"/register",
				})
		case "/register":
			registrations.Add(1)
			// If a second independent discovery were issued before the URL
			// consumer, this would now fail. The validated cached data wins.
			invalid.Store(true)
			w.WriteHeader(201)
			_, _ = w.Write([]byte("{\"client_id\":\"client\",\"client_secret\":\"synthetic-client-secret" +
				"\"}"))
		case "/token":
			exchanges.Add(1)
			_, _ = w.Write(
				[]byte(
					"{\"access_token\":\"synthetic-access-token\"," +
						"\"refresh_token\":\"synthetic-refresh-token\"," +
						"\"token_type\":\"Bearer\"}",
				),
			)
		}
	}))
	t.Cleanup(server.Close)
	u := server.URL + "/mcp"
	service := &types.MCPService{
		ID:       "cache",
		TenantID: 7,
		URL:      &u,
		AuthConfig: &types.MCPAuthConfig{
			AuthType:              types.MCPAuthOAuth,
			AuthServerMetadataURL: server.URL + "/metadata",
		},
	}
	manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "user"}
	url, state, err := manager.StartAuthorization(
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
	require.EqualValues(t, 1, discoveries.Load())
	require.EqualValues(t, 1, registrations.Load())
	// A callback constructs another handler and must validate its own cache.
	_, _, err = manager.CompleteAuthorization(context.Background(), state, "code")
	require.ErrorContains(t, err, "mcp_oauth_discovery_failed")
	require.Zero(t, exchanges.Load())
	// An existing registration must also be guarded (without registering again).
	url, state, err = manager.StartAuthorization(
		context.Background(),
		service,
		7,
		principal,
		"http://localhost/callback",
		"/",
	)
	require.ErrorContains(t, err, "mcp_oauth_discovery_failed")
	require.Empty(t, url)
	require.Empty(t, state)
	require.EqualValues(t, 1, registrations.Load())
	// Reauthorization after a provider recovery keeps the existing credential.
	invalid.Store(false)
	_, state, err = manager.StartAuthorization(
		context.Background(),
		service,
		7,
		principal,
		"http://localhost/callback",
		"/",
	)
	require.NoError(t, err)
	_, _, err = manager.CompleteAuthorization(context.Background(), state, "code")
	require.NoError(t, err)
	require.EqualValues(t, 1, registrations.Load())
	require.EqualValues(t, 1, exchanges.Load())
	stored, err := repo.GetClient(context.Background(), 7, service.ID)
	require.NoError(t, err)
	require.Equal(t, "synthetic-client-secret", stored.ClientSecret)
}

func TestOAuthRefreshBadDiscoveryPreservesToken(t *testing.T) {
	runtime, repo, requests, closeServer := newOAuthLifecycleFixture(t, 200, nil)
	defer closeServer()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(syntheticOAuthSensitive))
	}))
	defer bad.Close()
	h := transport.NewOAuthHandler(transport.OAuthConfig{AuthServerMetadataURL: bad.URL})
	err := runtime.ensureFresh(context.Background(), false, h)
	var temporary *OAuthRefreshTemporaryError
	require.ErrorAs(t, err, &temporary)
	require.ErrorContains(t, err, "mcp_oauth_discovery_failed")
	require.Zero(t, requests.Load())
	row, err := repo.GetTokenForPrincipal(
		context.Background(),
		7,
		types.Principal{Type: types.PrincipalWebUser, ID: "user-1"},
		"svc-1",
	)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", row.RefreshToken)
	leased, err := repo.TryAcquireTokenRefreshLease(
		context.Background(),
		7,
		types.Principal{Type: types.PrincipalWebUser, ID: "user-1"},
		"svc-1",
		"after-failure",
		time.Now().Add(time.Minute),
	)
	require.NoError(t, err)
	require.True(t, leased, "failed discovery must release the refresh lease")
}

func TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient(t *testing.T) {
	for _, mode := range []string{"same-origin", "cross-origin"} {
		t.Run(mode, func(t *testing.T) {
			db := securityOAuthDB(t)
			repo := repository.NewMCPOAuthRepository(db)
			utils.SetSSRFWhitelistFromRaw("127.0.0.1")
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			var foreignCalls atomic.Int32
			foreign := httptest.NewServer(
				http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { foreignCalls.Add(1) }),
			)
			t.Cleanup(foreign.Close)
			var registrations atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/redirect":
					target := server.URL + "/metadata"
					if mode == "cross-origin" {
						target = foreign.URL
					}
					http.Redirect(w, r, target, http.StatusFound)
				case "/register":
					registrations.Add(1)
				default:
					// registration_endpoint is optional for an existing public client.
					_ = json.NewEncoder(w).
						Encode(
							map[string]string{
								"authorization_endpoint": server.URL +
									"/authorize",

								"token_endpoint": server.URL +
									"/token",
							},
						)
				}
			}))
			t.Cleanup(server.Close)
			u := server.URL + "/mcp"
			service := &types.MCPService{
				ID:       "redirect",
				TenantID: 7,
				URL:      &u,
				AuthConfig: &types.MCPAuthConfig{
					AuthType:              types.MCPAuthOAuth,
					AuthServerMetadataURL: server.URL + "/redirect",
				},
			}
			require.NoError(
				t,
				repo.SaveClient(
					context.Background(),
					&types.MCPOAuthClient{TenantID: 7, ServiceID: service.ID, ClientID: "public"},
				),
			)
			manager := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
			url, state, err := manager.StartAuthorization(
				context.Background(),
				service,
				7,
				types.Principal{Type: types.PrincipalWebUser, ID: "user"},
				"http://localhost/callback",
				"/",
			)
			if mode == "same-origin" {
				require.NoError(t, err)
				require.NotEmpty(t, url)
				require.NotEmpty(t, state)
			} else {
				require.ErrorContains(t, err, "mcp_oauth_discovery_failed")
				require.Empty(t, url)
				require.Empty(t, state)
			}
			require.Zero(
				t,
				foreignCalls.Load(),
				"SSRF redirect policy must block before the foreign request",
			)
			require.Zero(t, registrations.Load(), "public client must not register again")
		})
	}
}
