package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type captureUsage struct{ reports []types.MCPUsageReport }

func TestMCPUsageAdmissionPreservesExecutionCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		sink := &captureUsage{}
		h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(r.Context())
				cancel()
			}
			defer cancel()
			captureUsageContextError(ctx)
			http.Error(w, "personal MCP denied", http.StatusForbidden)
		}))
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest("POST", "/mcp", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge"}}`,
		)))
		require.Equal(t, http.StatusForbidden, response.Code)
		require.Len(t, sink.reports, 1)
		require.False(t, sink.reports[0].Success)
		code := "canceled"
		if deadline {
			code = "timeout"
		}
		require.Equal(t, code, sink.reports[0].ErrorCode)
	}
}

func TestMCPUsageResourcesFollowDetachedAgentExecution(t *testing.T) {
	ctx, scope := types.WithMCPUsageScope(context.Background())
	types.ObserveMCPKnowledgeBase(logger.CloneContext(ctx), "executed-kb")
	require.Equal(t, []string{"executed-kb"}, scope.Targets())
	_, other := types.WithMCPUsageScope(context.Background())
	require.Empty(t, other.Targets(), "independent calls must never share observations")
}

func (s *captureUsage) Record(_ context.Context, report types.MCPUsageReport) error {
	s.reports = append(s.reports, report)
	return errors.New("offline")
}

func TestMCPUsageFinalResultAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *mcp.CallToolResult
		err    error
		code   string
	}{
		{"success", mcp.NewToolResultText("private answer"), nil, ""},
		{"is_error", mcp.NewToolResultError("private query failed"), nil, "tool_error"},
		{"denied", mcp.NewToolResultError("authorization denied"), nil, "permission_denied"},
		{"limited", mcp.NewToolResultError("rate limit exceeded"), nil, "rate_limited"},
		{"timeout", nil, context.DeadlineExceeded, "timeout"},
		{"canceled", nil, context.Canceled, "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &captureUsage{}
			request := mcp.CallToolRequest{}
			request.Params.Name = "search_knowledge"
			tool := observeTool(
				sink,
				"go_personal",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) { return tc.result, tc.err },
			)
			result, err := tool(context.Background(), request)
			require.Same(t, tc.result, result)
			require.Equal(t, tc.err, err)
			require.Len(t, sink.reports, 1)
			require.Equal(t, tc.code, sink.reports[0].ErrorCode)
			require.Equal(t, tc.code == "", sink.reports[0].Success)
			require.Equal(t, "unknown", sink.reports[0].ClientName)
		})
	}
}

func TestMCPUsageHTTPAdmissionAndSingleFinalEvent(t *testing.T) {
	for _, status := range []int{200, 403, 429} {
		sink := &captureUsage{}
		h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if status != 200 {
				w.WriteHeader(status)
				return
			}
			req := mcp.CallToolRequest{}
			req.Params.Name = "search_knowledge"
			_, _ = observeTool(
				sink,
				"go_personal",
				func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					for _, id := range []string{"a", "b", "c", "d", "e", "a"} {
						types.ObserveMCPKnowledgeBase(ctx, id)
					}
					return mcp.NewToolResultText(
						`{"results":[{"knowledge_base_id":"a","knowledge_id":"doc-a"},
						{"knowledge_base_id":"b","knowledge_id":"doc-b"}],
						"fallbacks":[{"knowledge_base_id":"c"}],"content":"private"}`,
					), nil
				},
			)(
				r.Context(),
				req,
			)
		}))
		h.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(
				"POST",
				"/mcp",
				strings.NewReader(
					`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge"}}`,
				),
			),
		)
		require.Len(t, sink.reports, 1)
		if status == 200 {
			require.Len(t, sink.reports[0].TargetKnowledgeBaseIDs, 5)
			require.ElementsMatch(t, []string{"a", "b"}, sink.reports[0].HitKnowledgeBaseIDs)
			require.Empty(t, sink.reports[0].KnowledgeIDs)
		}
	}
	for _, method := range []string{"initialize", "tools/list"} {
		sink := &captureUsage{}
		h := observeHTTP(sink, "go_endpoint", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		h.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(
				"POST",
				"/mcp",
				strings.NewReader(`{"method":"`+method+`","params":{"name":"secret"}}`),
			),
		)
		require.Empty(t, sink.reports)
	}
}

func TestMCPUsagePanicIsRecordedAndRethrown(t *testing.T) {
	sink := &captureUsage{}
	req := mcp.CallToolRequest{}
	req.Params.Name = "read_document"
	tool := observeTool(
		sink,
		"go_endpoint",
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) { panic("private") },
	)
	require.Panics(t, func() { _, _ = tool(context.Background(), req) })
	require.Len(t, sink.reports, 1)
	require.Equal(t, "panic", sink.reports[0].ErrorCode)
}
