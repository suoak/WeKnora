package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type lifecycleUsage struct {
	mu      sync.Mutex
	begins  map[string]int
	reports map[string]types.MCPUsageReport
	keys    map[string]uint64
	fail    bool
}

func (s *lifecycleUsage) Begin(_ context.Context, r types.MCPUsageReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begins[r.EventKey]++
	if s.fail {
		return errors.New("collector unavailable")
	}
	return nil
}
func (s *lifecycleUsage) Record(ctx context.Context, r types.MCPUsageReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports[r.EventKey] = r
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	s.keys[r.EventKey] = scope.KeyID
	if s.fail {
		return errors.New("collector unavailable")
	}
	return nil
}

func TestPersonalUsageConcurrentInvocationLifecycle(t *testing.T) {
	for _, failedRecorder := range []bool{false, true} {
		sink := &lifecycleUsage{begins: map[string]int{}, reports: map[string]types.MCPUsageReport{}, keys: map[string]uint64{}, fail: failedRecorder}
		h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mcp.CallToolRequest{}
			req.Params.Name = "add_document"
			result, err := observeTool(sink, "go_personal", func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("processing"), nil
			})(r.Context(), req)
			if err != nil || result.IsError {
				w.WriteHeader(500)
			}
		}))
		var wg sync.WaitGroup
		for i := 1; i <= 24; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{KeyID: uint64(i)})
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{"method":"tools/call","params":{"name":"add_document"}}`)).WithContext(ctx)
				r.Header.Set("User-Agent", fmt.Sprintf("codex/%d", i))
				h.ServeHTTP(httptest.NewRecorder(), r)
			}(i)
		}
		wg.Wait()
		require.Len(t, sink.reports, 24)
		require.Len(t, sink.begins, 24)
		for key, r := range sink.reports {
			require.Equal(t, 1, sink.begins[key])
			require.True(t, r.Success)
			require.Empty(t, r.ErrorCode)
			require.Equal(t, "go_personal", r.EntryPoint)
			require.Equal(t, "add_document", r.ToolName)
			require.Equal(t, "codex", r.ClientName)
			require.Equal(t, "user_agent", r.ClientSource)
			require.Equal(t, fmt.Sprint(sink.keys[key]), r.ClientVersion)
		}
	}
}

func TestPersonalUnknownToolRetainsName(t *testing.T) {
	sink := &captureUsage{}
	h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(`{"method":"tools/call","params":{"name":"unregistered_personal_tool"}}`)))
	require.Len(t, sink.reports, 1)
	require.False(t, sink.reports[0].Success)
	require.Equal(t, "unknown_tool", sink.reports[0].ErrorCode)
	require.Equal(t, "unregistered_personal_tool", sink.reports[0].ToolName)
}
