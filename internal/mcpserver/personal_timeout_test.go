package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func expiredPersonalContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

func requirePersonalTimeout(t *testing.T, raw string, id string) {
	t.Helper()
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &response))
	require.Equal(t, "2.0", response.JSONRPC)
	require.JSONEq(t, id, string(response.ID))
	require.Equal(t, -32000, response.Error.Code)
	require.Contains(t, response.Error.Message, "timed out")
}

// Exercise the real dependency, including the branch that suppresses a tool's
// error result after ctx.Done. No production service or credentials are used.
func TestPersonalRPCTimeoutRealTransport(t *testing.T) {
	for _, id := range []string{`"query-1"`, `9007199254740993`} {
		t.Run(id, func(t *testing.T) {
			m := server.NewMCPServer("timeout-test", "1")
			m.AddTool(mcp.NewTool("probe"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				<-ctx.Done()
				return mcp.NewToolResultError("INTERNAL_ERROR_MUST_NOT_ESCAPE"), nil
			})
			h := server.NewStreamableHTTPServer(m, server.WithStateLess(true))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest("POST", personalPath, strings.NewReader(
				`{"jsonrpc":"2.0","id":`+id+`,"method":"tools/call","params":{"name":"probe","arguments":{}}}`,
			)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			out := httptest.NewRecorder()
			w := newPersonalRPCResponse(out, json.RawMessage(id))
			h.ServeHTTP(w, req)
			w.finish(ctx)
			require.Equal(t, http.StatusGatewayTimeout, out.Code)
			require.Equal(t, "application/json", out.Header().Get("Content-Type"))
			require.Equal(t, "request_timeout", out.Header().Get("X-KnowHub-Error-Code"))
			requirePersonalTimeout(t, out.Body.String(), id)
			require.NotContains(t, out.Body.String(), "INTERNAL_ERROR")
		})
	}
}

func TestPersonalRPCTimeoutHandlerAndAccounting(t *testing.T) {
	s, _, ctx := selectedMCPFixture()
	sink := &captureUsage{}
	s.usage = sink
	called := false
	s.handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", personalPath, strings.NewReader(
		`{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"knowledge_base_ids":["selected"],"query":"probe"}}}`,
	)).WithContext(ctx)
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, req)
	require.True(t, called)
	require.Equal(t, http.StatusGatewayTimeout, out.Code)
	requirePersonalTimeout(t, out.Body.String(), "17")
	require.Len(t, sink.reports, 1)
	require.False(t, sink.reports[0].Success)
	require.Equal(t, "timeout", sink.reports[0].ErrorCode)
	require.Zero(t, s.admission.snapshot("A").Active)
}

func TestPersonalRPCTimeoutPreservesCommittedResponses(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`,
	} {
		out := httptest.NewRecorder()
		w := newPersonalRPCResponse(out, json.RawMessage(`1`))
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
		// A deadline that expires after the result must not produce two replies.
		w.finish(expiredPersonalContext(t))
		require.Equal(t, 200, out.Code)
		require.Equal(t, body, out.Body.String())
	}
	for _, status := range []int{403, 429, 504} {
		out := httptest.NewRecorder()
		w := newPersonalRPCResponse(out, json.RawMessage(`1`))
		w.WriteHeader(status)
		w.finish(expiredPersonalContext(t))
		require.Equal(t, status, out.Code)
		require.Empty(t, out.Body.String())
	}
}

func TestPersonalRPCTimeoutSSEProgressAndFinalResponse(t *testing.T) {
	for _, final := range []bool{false, true} {
		t.Run(map[bool]string{false: "progress-only", true: "already-complete"}[final], func(t *testing.T) {
			out := httptest.NewRecorder()
			w := newPersonalRPCResponse(out, json.RawMessage(`"sse"`))
			w.Header().Set("Content-Type", "text/event-stream")
			w.Flush()
			progress := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n"
			// Write boundaries need not coincide with SSE frame boundaries.
			_, err := w.Write([]byte(progress[:30]))
			require.NoError(t, err)
			_, err = w.Write([]byte(progress[30:]))
			require.NoError(t, err)
			want := progress
			if final {
				frame := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"sse\",\"result\":{}}\n\n"
				_, err = w.Write([]byte(frame))
				require.NoError(t, err)
				want += frame
			}
			w.finish(expiredPersonalContext(t))
			require.Equal(t, 200, out.Code, "cannot replace status after flushing SSE")
			if final {
				require.Equal(t, want, out.Body.String())
			} else {
				tail := strings.TrimPrefix(out.Body.String(), progress+"event: message\ndata: ")
				requirePersonalTimeout(t, strings.TrimSpace(tail), `"sse"`)
			}
		})
	}
}

func TestPersonalRPCTimeoutDoesNotReplyToNotificationsOrCancellation(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx context.Context
		id  json.RawMessage
	}{
		{canceled, json.RawMessage(`1`)},
		{expiredPersonalContext(t), nil},
		{context.Background(), json.RawMessage(`1`)},
	} {
		out := httptest.NewRecorder()
		newPersonalRPCResponse(out, tc.id).finish(tc.ctx)
		require.Empty(t, out.Body.String())
	}
}
