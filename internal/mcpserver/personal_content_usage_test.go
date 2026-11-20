package mcpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestContentUsageOriginalCause(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{access.ErrForbidden, "permission_denied"},
		{service.ErrPersonalContentArguments, "invalid_arguments"},
		{service.ErrPersonalUpload, "invalid_arguments"},
		{service.ErrPersonalContentConflict, "invalid_arguments"},
		{errors.New("database connection failed"), "execution_error"},
		{service.ErrPersonalScanner, "execution_error"},
		{&net.DNSError{IsTimeout: true}, "timeout"},
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "canceled"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			sink := &captureUsage{}
			req := mcp.CallToolRequest{}
			req.Params.Name = "add_document"
			_, err := observeTool(sink, "go_personal", func(
				ctx context.Context, _ mcp.CallToolRequest,
			) (*mcp.CallToolResult, error) {
				return contentFailure(ctx, tc.cause, "creation denied or failed"), nil
			})(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, sink.reports, 1)
			require.False(t, sink.reports[0].Success)
			require.Equal(t, tc.code, sink.reports[0].ErrorCode)
		})
	}
}

func TestPersonalWikiRegexUsageInvalidArguments(t *testing.T) {
	for _, tool := range []string{"wiki_search", "wiki_read_page", "wiki_index"} {
		t.Run(tool, func(t *testing.T) {
			sink := &captureUsage{}
			s := &PersonalServer{}
			req := mcp.CallToolRequest{}
			req.Params.Name = tool
			req.Params.Arguments = map[string]any{"regex": true}
			result, err := observeTool(sink, "go_personal", s.call)(context.Background(), req)
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Equal(t, "Personal wiki_search supports bounded literal text; regex is not available", result.Content[0].(mcp.TextContent).Text)
			require.Len(t, sink.reports, 1)
			require.False(t, sink.reports[0].Success)
			require.Equal(t, "invalid_arguments", sink.reports[0].ErrorCode)
		})
	}
}

func TestPersonalLargeUsageBodyReplayAndLimit(t *testing.T) {
	for _, size := range []int{1<<20 + 1, 8 << 20, 12 << 20, 12<<20 + 1} {
		prefix := `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
			`"params":{"name":"upload_document","arguments":{"content_base64":"`
		suffix := `"}}}`
		body := prefix + strings.Repeat("A", size-len(prefix)-len(suffix)) + suffix
		sink := &captureUsage{}
		called := false
		h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			// The downstream parser must receive every byte exactly once.
			var b strings.Builder
			_, err := io.Copy(&b, r.Body)
			require.NoError(t, err)
			require.Equal(t, body, b.String())
			req := mcp.CallToolRequest{}
			req.Params.Name = "upload_document"
			_, _ = observeTool(sink, "go_personal", func(
				context.Context, mcp.CallToolRequest,
			) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("processing"), nil
			})(r.Context(), req)
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		require.Len(t, sink.reports, 1)
		require.Equal(t, "upload_document", sink.reports[0].ToolName)
		require.Equal(t, size <= 12<<20, called)
		require.Equal(t, size <= 12<<20, sink.reports[0].Success)
		if size > 12<<20 {
			require.Equal(t, 413, w.Code)
			require.Equal(t, "invalid_arguments", sink.reports[0].ErrorCode)
		}
	}
}
