package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/stretchr/testify/require"
)

// Exercise actual Streamable HTTP registration. Authorization and content are
// tested separately through the authenticated APP, never bypassed by this test.
func TestV1NextEightToolProtocolRegistration(t *testing.T) {
	s := NewPersonalServer(false, nil)
	request := httptest.NewRequest("POST", "/api/v1/mcp/personal", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, request)
	require.Equal(t, 200, w.Code)
	var body struct {
		Result struct {
			Tools []struct {
				Name        string
				Annotations struct {
					ReadOnlyHint bool `json:"readOnlyHint"`
				}
			}
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	names := []string{}
	for _, tool := range body.Result.Tools {
		names = append(names, tool.Name)
		require.True(t, tool.Annotations.ReadOnlyHint, tool.Name)
	}
	sort.Strings(names)
	require.Equal(t, []string{"grep_chunks", "list_documents", "list_knowledge_bases", "read_document", "search_knowledge", "wiki_index", "wiki_read_page", "wiki_search"}, names)
	// The public disabled handler still fails even though tools are registered.
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	require.Equal(t, 403, w.Code)
}

func TestV1NextPersonalConcurrencyIsBounded(t *testing.T) {
	s := NewPersonalServer(true, &service.PortalMCPKnowledge{})
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mcp/personal", bytes.NewBufferString(`{}`)))
	require.Equal(t, 429, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Equal(t, cap(s.slots), len(s.slots), "rejected request does not consume a slot")
}
