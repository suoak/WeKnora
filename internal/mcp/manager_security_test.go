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
)

func TestBuiltinOAuthTenantConnectionIsolation(t *testing.T) {
	db := securityOAuthDB(t)
	repo := repository.NewMCPOAuthRepository(db)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	endpoint := &expiringSessionServer{}
	var mu sync.Mutex
	headers := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		mu.Lock()
		headers[request.Method] = append(headers[request.Method], r.Header.Get("Authorization"))
		mu.Unlock()
		endpoint.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	service := &types.MCPService{
		ID:            "builtin",
		TenantID:      99,
		IsBuiltin:     true,
		Enabled:       true,
		URL:           &server.URL,
		TransportType: types.MCPTransportHTTPStreamable,
		AuthConfig:    &types.MCPAuthConfig{AuthType: types.MCPAuthOAuth},
	}
	alice := types.Principal{Type: types.PrincipalWebUser, ID: "same-user"}
	bob := types.Principal{Type: types.PrincipalWebUser, ID: "other-user"}
	ctxA := types.WithPrincipal(types.WithExecutionTenant(context.Background(), 7), alice)
	ctxB := types.WithPrincipal(types.WithExecutionTenant(context.Background(), 8), alice)
	ctxOther := types.WithPrincipal(types.WithExecutionTenant(context.Background(), 7), bob)
	for _, scope := range []struct {
		tenant    uint64
		principal types.Principal
		token     string
	}{{7, alice, "tenant-a-token"}, {8, alice, "tenant-b-token"}, {7, bob, "other-principal-token"}} {
		require.NoError(
			t,
			repo.SaveClient(
				context.Background(),
				&types.MCPOAuthClient{
					TenantID:  scope.tenant,
					ServiceID: service.ID,
					ClientID:  "public-client",
				},
			),
		)
		require.NoError(
			t,
			repo.SaveTokenForPrincipal(
				context.Background(),
				&types.MCPOAuthToken{
					TenantID:      scope.tenant,
					ServiceID:     service.ID,
					PrincipalType: scope.principal.Type,
					PrincipalID:   scope.principal.ID,
					UserID:        scope.principal.StorageID(),
					AccessToken:   scope.token,
					TokenType:     "Bearer",
					ExpiresAt:     time.Now().Add(time.Hour),
				},
			),
		)
	}
	manager := NewMCPManager(repo)
	t.Cleanup(manager.Shutdown)
	a, err := manager.GetOrCreateClient(ctxA, service)
	require.NoError(t, err)
	b, err := manager.GetOrCreateClient(ctxB, service)
	require.NoError(t, err)
	require.NotSame(t, a, b)
	aRuntime := a.(*managedMCPClient).MCPClient.(*mcpGoClient).oauth
	bRuntime := b.(*managedMCPClient).MCPClient.(*mcpGoClient).oauth
	require.NotSame(t, aRuntime.handler, bRuntime.handler)
	require.EqualValues(t, 7, aRuntime.tenantID)
	require.EqualValues(t, 8, bRuntime.tenantID)
	require.Equal(t, alice.StorageID(), aRuntime.principal.StorageID())
	other, err := manager.GetOrCreateClient(ctxOther, service)
	require.NoError(t, err)
	require.NotSame(t, a, other)
	require.EqualValues(t, 3, endpoint.initializations.Load())
	mu.Lock()
	initialHeaders := append([]string(nil), headers["initialize"]...)
	mu.Unlock()
	require.Equal(
		t,
		[]string{"Bearer tenant-a-token", "Bearer tenant-b-token", "Bearer other-principal-token"},
		initialHeaders,
	)
	t.Logf(
		"cache scopes: A=%q B=%q principal=%q; actual initialize Authorization "+
			"scopes verified",
		cacheKey(service, 7, alice),
		cacheKey(service, 8, alice),
		cacheKey(service, 7, bob),
	)
	var wg sync.WaitGroup
	results := make(chan MCPClient, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c, err := manager.GetOrCreateClient(ctxB, service); results <- c; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for c := range results {
		require.Same(t, b, c)
	}
	require.EqualValues(t, 3, endpoint.initializations.Load())
	require.NoError(t, manager.CloseOAuthClient(7, alice, service.ID))
	require.False(t, a.IsConnected())
	require.True(t, b.IsConnected())
	require.True(t, other.IsConnected())
	again, err := manager.GetOrCreateClient(ctxB, service)
	require.NoError(t, err)
	require.Same(t, b, again)
	// A lost transport disconnects its own object, never another cache scope.
	endpoint.expired.Store(true)
	_, err = b.ListTools(ctxB)
	require.Error(t, err)
	require.False(t, b.IsConnected())
	require.True(t, other.IsConnected())
	endpoint.expired.Store(false)
	fresh, err := manager.GetOrCreateClient(ctxB, service)
	require.NoError(t, err)
	require.NotSame(t, b, fresh)
	for _, status := range []int32{401, 403} {
		require.NoError(
			t,
			repo.SaveTokenForPrincipal(
				context.Background(),
				&types.MCPOAuthToken{
					TenantID:      7,
					ServiceID:     service.ID,
					PrincipalType: bob.Type,
					PrincipalID:   bob.ID,
					UserID:        bob.StorageID(),
					AccessToken:   "other-principal-token",
					TokenType:     "Bearer",
					ExpiresAt:     time.Now().Add(time.Hour),
				},
			),
		)
		endpoint.status.Store(status)
		before := endpoint.toolCalls.Load()
		_, err = other.CallTool(ctxOther, "side-effect", nil)
		require.Error(t, err)
		require.Equal(
			t,
			before+1,
			endpoint.toolCalls.Load(),
			"permission failure cannot replay a tool without a refresh credential",
		)
		require.True(t, fresh.IsConnected())
	}
	require.NoError(t, manager.CloseClient(service.ID))
	require.Zero(t, manager.GetActiveClients())
}

type observedWaiterContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *observedWaiterContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestManagerRealWaiterDeadlineDoesNotCancelInitialization(t *testing.T) {
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	started, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	endpoint := &expiringSessionServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		if request.Method == "initialize" {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		endpoint.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	manager := NewMCPManager(nil)
	t.Cleanup(manager.Shutdown)
	service := &types.MCPService{
		ID:            "deadline",
		Enabled:       true,
		URL:           &server.URL,
		TransportType: types.MCPTransportHTTPStreamable,
	}
	type result struct {
		client MCPClient
		err    error
	}
	success := make(chan result, 4)
	go func() { c, err := manager.GetOrCreateClient(context.Background(), service); success <- result{c, err} }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("initialization not started")
	}
	deadline, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	expired := make(chan error, 1)
	waiter := &observedWaiterContext{Context: deadline, entered: make(chan struct{})}
	go func() { _, err := manager.GetOrCreateClient(waiter, service); expired <- err }()
	select {
	case <-waiter.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline waiter never joined pending connection")
	}
	for i := 0; i < 3; i++ {
		waiting := &observedWaiterContext{Context: context.Background(), entered: make(chan struct{})}
		go func() { c, err := manager.GetOrCreateClient(waiting, service); success <- result{c, err} }()
		select {
		case <-waiting.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("waiter did not join")
		}
	}
	select {
	case err := <-expired:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("deadline waiter blocked")
	}
	manager.clientsMu.RLock()
	pending := len(manager.connecting)
	manager.clientsMu.RUnlock()
	require.Equal(t, 1, pending)
	releaseOnce.Do(func() { close(release) })
	var first MCPClient
	for i := 0; i < 4; i++ {
		select {
		case r := <-success:
			require.NoError(t, r.err)
			if first == nil {
				first = r.client
			} else {
				require.Same(t, first, r.client)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("surviving waiter blocked")
		}
	}
	require.EqualValues(t, 1, endpoint.initializations.Load())
	manager.clientsMu.RLock()
	pending = len(manager.connecting)
	manager.clientsMu.RUnlock()
	require.Zero(t, pending)
	require.NoError(t, manager.CloseClient(service.ID))
	require.False(t, first.IsConnected())
	require.Zero(t, manager.GetActiveClients())
}

func TestOAuthCallbackInvalidatesOnlyAuthenticatedStateScope(t *testing.T) {
	db := securityOAuthDB(t)
	repo := repository.NewMCPOAuthRepository(db)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	endpoint := &expiringSessionServer{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
			_ = json.NewEncoder(w).
				Encode(
					map[string]any{
						"issuer": server.URL,

						"registration_endpoint": server.URL +
							"/register",

						"authorization_endpoint": server.URL +
							"/authorize",

						"token_endpoint": server.URL +
							"/token",
					},
				)
		case "/register":
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "public-client"})
		case "/token":
			_ = r.ParseForm()
			_ = json.NewEncoder(w).
				Encode(map[string]any{"access_token": r.Form.Get("code"), "token_type": "Bearer", "expires_in": 3600})
		default:
			endpoint.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	u := server.URL + "/mcp"
	service := &types.MCPService{
		ID:            "builtin-callback",
		TenantID:      99,
		IsBuiltin:     true,
		Enabled:       true,
		URL:           &u,
		TransportType: types.MCPTransportHTTPStreamable,
		AuthConfig: &types.MCPAuthConfig{
			AuthType:              types.MCPAuthOAuth,
			AuthServerMetadataURL: server.URL + "/metadata",
		},
	}
	alice := types.Principal{Type: types.PrincipalWebUser, ID: "same-user"}
	bob := types.Principal{Type: types.PrincipalWebUser, ID: "other-user"}
	oauth := NewOAuthManager(repo, fakeMCPServiceRepo{service: service}, nil)
	manager := NewMCPManager(repo)
	t.Cleanup(manager.Shutdown)
	ctx := context.Background()
	for _, scope := range []struct {
		tenant uint64
		code   string
	}{{7, "synthetic-a"}, {8, "synthetic-b"}} {
		_, state, err := oauth.StartAuthorization(
			ctx,
			service,
			scope.tenant,
			alice,
			"http://localhost/callback",
			"/",
		)
		require.NoError(t, err)
		_, _, err = oauth.CompleteAuthorization(ctx, state, scope.code)
		require.NoError(t, err)
	}
	ctxA := types.WithPrincipal(types.WithExecutionTenant(ctx, 7), alice)
	ctxB := types.WithPrincipal(types.WithExecutionTenant(ctx, 8), alice)
	ctxBob := types.WithPrincipal(types.WithExecutionTenant(ctx, 7), bob)
	a, err := manager.GetOrCreateClient(ctxA, service)
	require.NoError(t, err)
	b, err := manager.GetOrCreateClient(ctxB, service)
	require.NoError(t, err)
	require.NoError(
		t,
		repo.SaveTokenForPrincipal(
			ctx,
			&types.MCPOAuthToken{
				TenantID:      7,
				ServiceID:     service.ID,
				PrincipalType: bob.Type,
				PrincipalID:   bob.ID,
				UserID:        bob.StorageID(),
				AccessToken:   "synthetic-bob",
				TokenType:     "Bearer",
				ExpiresAt:     time.Now().Add(time.Hour),
			},
		),
	)
	other, err := manager.GetOrCreateClient(ctxBob, service)
	require.NoError(t, err)
	_, state, err := oauth.StartAuthorization(ctx, service, 7, alice, "http://localhost/callback", "/")
	require.NoError(t, err)
	_, _, err = oauth.CompleteAuthorizationAndInvalidate(ctx, state, "synthetic-a-new", manager)
	require.NoError(t, err)
	require.False(t, a.IsConnected())
	require.True(t, b.IsConnected())
	require.True(t, other.IsConnected())
	tokenA, err := repo.GetTokenForPrincipal(ctx, 7, alice, service.ID)
	require.NoError(t, err)
	require.Equal(t, "synthetic-a-new", tokenA.AccessToken)
	tokenB, err := repo.GetTokenForPrincipal(ctx, 8, alice, service.ID)
	require.NoError(t, err)
	require.Equal(t, "synthetic-b", tokenB.AccessToken)
}
