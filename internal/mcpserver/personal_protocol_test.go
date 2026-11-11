package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestPersonalRegistryNeverInheritsWorkspaceOrFutureWriteTools(t *testing.T) {
	s := NewPersonalServer(true, &service.PortalMCPKnowledge{})
	for _, name := range []string{
		"ask", "add_document", "update_document", "delete_document", "upload_file", "future_internal_upload",
		"http_proxy", "execute_agent", "wiki_create", "chunk_update", "faq_import",
	} {
		nextCalls := 0
		h := s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			nextCalls++
			return mcp.NewToolResultText("unexpected write"), nil
		})
		var req mcp.CallToolRequest
		req.Params.Name = name
		res, err := h(context.Background(), req)
		require.NoError(t, err)
		require.True(t, res.IsError)
		require.Zero(t, nextCalls)
	}
}

func TestPersonalProtocolRejectsPayloadBeforeDispatch(t *testing.T) {
	canary := "SYNTHETIC_UPLOAD_MUST_NOT_DISPATCH"
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"question":"` +
			canary + `"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_document","arguments":{"content":"` +
			canary + `"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge",` +
			`"arguments":{"query":"ok","content":"` + canary + `"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_document",` +
			`"arguments":{"knowledge_id":"ok","path":"/etc/passwd"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"tools/call"}`,
		`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`,
		`{"jsonrpc":"2.0","id":1,"method":"rest/proxy","params":{"url":"http://internal"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wiki_search",` +
			`"arguments":{"query":"ok","regex":true}}}`,
	} {
		called := 0
		h := personalHTTPBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called++
			w.WriteHeader(200)
		}), isolatedPersonalPath)
		r := httptest.NewRequest("POST", isolatedPersonalPath, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
		require.Zero(t, called)
		require.NotContains(t, w.Body.String(), canary)
	}
}

func TestPersonalProtocolNormalJSONAndExactLimit(t *testing.T) {
	for _, media := range []string{"application/json", "application/json; charset=utf-8"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
		for _, size := range []int{len(body), 1 << 20, (1 << 20) + 1} {
			t.Run(fmt.Sprint(media, size), func(t *testing.T) {
				called := 0
				h := personalHTTPBoundary(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					require.True(t, logger.PrivatePayload(r.Context()))
					require.True(t, logger.IsolatedPersonalPayload(r.Context()))
					called++
				}), isolatedPersonalPath)
				payload := body + strings.Repeat(" ", size-len(body))
				r := httptest.NewRequest("POST", isolatedPersonalPath, strings.NewReader(payload))
				r.ContentLength = -1 // covers an unknown-length/chunked body too
				r.Header.Set("Content-Type", media)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if size > 1<<20 {
					require.Equal(t, 413, w.Code)
					require.Zero(t, called)
				} else {
					require.Equal(t, 200, w.Code)
					require.Equal(t, 1, called)
				}
			})
		}
	}
}

func TestPersonalProtocolPathMediaAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		method, path, media, origin, body string
		status                            int
	}{
		{"POST", isolatedPersonalPath + "?content=secret", "application/json", "", `{}`, 404},
		{"POST", isolatedPersonalPath + "/", "application/json", "", `{}`, 404},
		{"POST", "/api/v1/mcp/%70ersonal", "application/json", "", `{}`, 404},
		{"PUT", isolatedPersonalPath, "application/json", "", `{}`, 405},
		{"POST", isolatedPersonalPath, "multipart/form-data; boundary=x", "", "--x", 415},
		{"POST", isolatedPersonalPath, "application/octet-stream", "", "binary", 415},
		{"POST", isolatedPersonalPath, "application/json", "https://attacker.invalid", `{}`, 403},
		{"GET", isolatedPersonalPath, "", "", "payload", 400},
	} {
		h := personalHTTPBoundary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("rejected payload dispatched")
		}), isolatedPersonalPath)
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.media)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code)
	}
}
