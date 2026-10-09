package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mark3labs/mcp-go/client/transport"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

// expiringSessionServer is a Streamable HTTP MCP server on a session-based
// protocol version that forgets the session after initialization: the next
// request gets a 404, as after a server restart.
type expiringSessionServer struct {
	expired         atomic.Bool
	initializations atomic.Int32
	toolCalls       atomic.Int32
	status          atomic.Int32
	noSession       bool
}

func (s *expiringSessionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(body, &request)
	if request.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := func(message map[string]any) {
		message["jsonrpc"] = "2.0"
		message["id"] = request.ID
		w.Header().Set("Content-Type", "application/json")
		if !s.noSession {
			w.Header().Set("Mcp-Session-Id", fmt.Sprintf("session-%d", s.initializations.Load()))
		}
		_ = json.NewEncoder(w).Encode(message)
	}
	switch request.Method {
	case "server/discover":
		reply(map[string]any{"error": map[string]any{"code": -32601, "message": "Method not found"}})
	case "initialize":
		s.initializations.Add(1)
		reply(map[string]any{"result": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "expiring", "version": "1.0.0"},
		}})
	default:
		if request.Method == "tools/call" {
			s.toolCalls.Add(1)
		}
		if code := s.status.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		if s.expired.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(map[string]any{"result": map[string]any{"tools": []any{}}})
	}
}

// A 404 for the session means the server no longer knows it, and the spec
// has the client start a new session. mcp-go reports it as
// transport.ErrSessionTerminated. The client disconnects on it, so that
// GetOrCreateClient builds a fresh connection instead of handing out the
// dead session again.
func TestMCPClientDisconnectsWhenTheSessionIsGone(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	server := &expiringSessionServer{}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	serviceURL := httpServer.URL
	client, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{
		ID:            "svc-1",
		TransportType: types.MCPTransportHTTPStreamable,
		URL:           &serviceURL,
	}})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Disconnect() }()
	_, err = client.Initialize(ctx)
	require.NoError(t, err)
	_, err = client.ListTools(ctx)
	require.NoError(t, err)
	require.True(t, client.IsConnected())

	server.expired.Store(true)
	_, err = client.ListTools(ctx)
	require.Error(t, err)
	require.False(t, client.IsConnected(), "the client should disconnect so the next use reconnects")
}

// Generic HTTP errors must not be treated as a session loss. A stateless SDK
// POST 404 has the same sentinel, so only a negotiated session qualifies.
func TestMCPSessionErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name                string
		err                 error
		session, disconnect bool
	}{
		{"session-404", transport.NewError(transport.ErrSessionTerminated), true, true},
		{"wrapped-session", fmt.Errorf("wrap: %w", transport.NewError(fmt.Errorf("wrap: %w", transport.ErrSessionTerminated))), true, true},
		{"stateless-404", transport.NewError(transport.ErrSessionTerminated), false, false},
		{"ordinary-404", transport.NewError(errors.New("HTTP 404 not found")), true, false},
		{"unauthorized", transport.NewError(errors.New("HTTP 401 Unauthorized")), true, false},
		{"forbidden", transport.NewError(errors.New("HTTP 403 Forbidden")), true, false},
		{"cancelled", transport.NewError(context.Canceled), true, false},
		{"timeout", transport.NewError(context.DeadlineExceeded), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &mcpGoClient{}
			c.connected.Store(true)
			c.initialized.Store(true)
			c.sessionBased.Store(tc.session)
			c.checkErrorAndDisconnectIfNeeded(tc.err)
			require.Equal(t, !tc.disconnect, c.IsConnected())
			require.Equal(t, !tc.disconnect, c.initialized.Load())
		})
	}
}

func TestMCPManagerReconnectsWithoutReplayingToolCall(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := &expiringSessionServer{}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	u := httpServer.URL
	service := &types.MCPService{ID: "session-service", TenantID: 7, Enabled: true, TransportType: types.MCPTransportHTTPStreamable, URL: &u}
	manager := NewMCPManager(nil)
	defer manager.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	old, err := manager.GetOrCreateClient(ctx, service)
	require.NoError(t, err)
	reused, err := manager.GetOrCreateClient(ctx, service)
	require.NoError(t, err)
	require.Same(t, old, reused)
	server.expired.Store(true)
	_, err = old.CallTool(ctx, "side-effect", nil)
	require.Error(t, err)
	require.EqualValues(t, 1, server.toolCalls.Load(), "failed tool operation must not be replayed")
	require.False(t, old.IsConnected())
	server.expired.Store(false)
	var wg sync.WaitGroup
	clients := make(chan MCPClient, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c, err := manager.GetOrCreateClient(ctx, service); clients <- c; errs <- err }()
	}
	wg.Wait()
	close(clients)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var fresh MCPClient
	for c := range clients {
		if fresh == nil {
			fresh = c
		} else {
			require.Same(t, fresh, c)
		}
	}
	require.NotSame(t, old, fresh)
	require.True(t, fresh.IsConnected())
	require.EqualValues(t, 2, server.initializations.Load(), "concurrent reconnect must initialize once")
	require.EqualValues(t, 1, server.toolCalls.Load(), "reconnecting alone must not repeat the failed tool")
	cancelled, stop := context.WithCancel(ctx)
	stop()
	_, err = manager.GetOrCreateClient(cancelled, service)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, fresh.IsConnected(), "one cancelled caller must not kill the shared connection")
}

func TestMCPStateless404DoesNotResetConnection(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := &expiringSessionServer{noSession: true}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	u := httpServer.URL
	c, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{ID: "stateless", TransportType: types.MCPTransportHTTPStreamable, URL: &u}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	defer func() { _ = c.Disconnect() }()
	_, err = c.Initialize(ctx)
	require.NoError(t, err)
	server.expired.Store(true)
	_, err = c.ListTools(ctx)
	require.Error(t, err)
	require.True(t, c.IsConnected())
}

func TestMCPHTTPAuthErrorsAreNotRetried(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := &expiringSessionServer{}
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			u := httpServer.URL
			c, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{ID: "auth-boundary", TransportType: types.MCPTransportHTTPStreamable, URL: &u}})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, c.Connect(ctx))
			defer func() { _ = c.Disconnect() }()
			_, err = c.Initialize(ctx)
			require.NoError(t, err)
			server.status.Store(int32(code))
			_, err = c.CallTool(ctx, "side-effect", nil)
			require.Error(t, err)
			require.EqualValues(t, 1, server.toolCalls.Load())
			require.True(t, c.IsConnected(), "auth rejection must not be mistaken for session loss")
		})
	}
}
