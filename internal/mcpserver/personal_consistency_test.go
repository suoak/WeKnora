package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalConsistencyOptionalTargets(t *testing.T) {
	for _, tool := range NewPersonalServer(false, nil).readTools() {
		if property, ok := tool.InputSchema.Properties["knowledge_base_ids"].(map[string]any); ok {
			require.ElementsMatch(t, []string{"array", "null"}, property["type"])
		}
	}
	for _, tool := range []string{"search_knowledge", "grep_chunks", "search_documents", "wiki_search"} {
		for _, args := range []map[string]any{
			{},
			{"knowledge_base_ids": nil},
			{"knowledge_base_ids": []any{}},
			{"knowledge_base_ids": []string{}},
			{"knowledge_base_ids": []any{"kb"}},
		} {
			_, e := personalOperationTargets(tool, args)
			require.NoError(t, e)
		}
		for _, value := range []any{"kb", 4, true, []any{nil}, []any{1}, []any{" "}} {
			_, e := personalOperationTargets(tool, map[string]any{"knowledge_base_ids": value})
			require.Error(t, e)
		}
	}
}

func TestPersonalConsistencyJSONRPC(t *testing.T) {
	f := fixture.New(t)
	knowledge := &service.PortalMCPKnowledge{
		Resolver:  f.Portal(t),
		Knowledge: repository.NewKnowledgeRepository(f.DB),
		Chunks:    repository.NewChunkRepository(f.DB),
	}
	s := NewPersonalServer(true, knowledge)
	call := func(method string, params any) map[string]any {
		body, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		require.NoError(t, e)
		request := httptest.NewRequest("POST", personalPath, bytes.NewReader(body)).
			WithContext(fixture.Key("A", 1, 1, true))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, request)
		require.Equal(t, 200, w.Code, w.Body.String())
		var response map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Nil(t, response["error"])
		isolatedRequest := httptest.NewRequest("POST", isolatedPersonalPath, bytes.NewReader(body)).
			WithContext(fixture.Key("A", 1, 1, true))
		isolatedRequest.Header.Set("Content-Type", "application/json")
		isolatedRequest.Header.Set("Accept", "application/json, text/event-stream")
		isolatedResponse := httptest.NewRecorder()
		s.IsolatedHandler().ServeHTTP(isolatedResponse, isolatedRequest)
		require.Equal(t, 200, isolatedResponse.Code, isolatedResponse.Body.String())
		var isolated map[string]any
		require.NoError(t, json.Unmarshal(isolatedResponse.Body.Bytes(), &isolated))
		require.Nil(t, isolated["error"])
		require.NotEqual(t, true, isolated["result"].(map[string]any)["isError"])
		return response["result"].(map[string]any)
	}
	initialized := call(
		"initialize",
		map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "DeepSeek-Harness-format-probe", "version": "1"},
		},
	)
	require.Contains(t, initialized["instructions"], "search_documents")
	listed := call("tools/list", map[string]any{})
	found := false
	for _, entry := range listed["tools"].([]any) {
		tool := entry.(map[string]any)
		if tool["name"] == "search_documents" {
			found = true
			require.Equal(t, true, tool["annotations"].(map[string]any)["readOnlyHint"])
		}
	}
	require.True(t, found)
	for _, selection := range []any{nil, []any{}} {
		result := call(
			"tools/call",
			map[string]any{
				"name":      "search_documents",
				"arguments": map[string]any{"query": "PUBLIC", "knowledge_base_ids": selection},
			},
		)
		require.NotEqual(t, true, result["isError"])
		structured := result["structuredContent"].(map[string]any)
		var text map[string]any
		require.NoError(
			t,
			json.Unmarshal([]byte(result["content"].([]any)[0].(map[string]any)["text"].(string)), &text),
		)
		require.Equal(t, text, structured)
		require.Contains(t, structured, "documents")
	}
	// A live source exclusion must be explicit in the object itself, including
	// when no document remains. Clients may consume only structuredContent.
	restricted, store, ctx := selectedMCPFixture()
	restricted.knowledge.Knowledge = repository.NewKnowledgeRepository(f.DB)
	restricted.knowledge.Resolver.Store = partialTitleStore{store}
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	scope.KnowledgeBaseRestricted = false
	scope.KnowledgeBaseIDs = nil
	ctx = types.WithTenantAPIKeyScope(ctx, scope)
	partial, err := restricted.knowledge.SearchDocuments(ctx, nil, "PUBLIC", 20)
	require.NoError(t, err)
	require.Equal(t, "partial", partial.Status)
	require.Empty(t, partial.Documents)
}

func TestPersonalConsistencyIsolatedBoundary(t *testing.T) {
	for _, tool := range []string{"search_documents", "search_knowledge", "grep_chunks", "wiki_search"} {
		for _, selection := range []any{nil, []any{}, []any{"kb"}, "kb", true, []any{nil}, []any{1}} {
			args := map[string]any{"query": "HA", "knowledge_base_ids": selection}
			body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": tool, "arguments": args}})
			require.NoError(t, err)
			called := false
			h := personalHTTPBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}), isolatedPersonalPath)
			request := httptest.NewRequest("POST", isolatedPersonalPath, bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			valid := selection == nil
			if ids, ok := selection.([]any); ok {
				valid = len(ids) == 0
				if len(ids) == 1 {
					_, valid = ids[0].(string)
				}
			}
			require.Equal(t, valid, called, "%s: %s", tool, body)
			if valid {
				require.Equal(t, http.StatusOK, response.Code)
			} else {
				require.Equal(t, http.StatusBadRequest, response.Code)
			}
		}
	}
	for _, args := range []string{`{"query":"HA","content":"write"}`, `{"query":"HA","path":"/etc/passwd"}`} {
		require.Error(t, validatePersonalRPC([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_documents","arguments":`+args+`}}`)))
	}
}

type partialTitleStore struct{ *selectedMCPStore }

func (s partialTitleStore) LoadDocumentReadSnapshot(
	ctx context.Context, user string, key uint64, kb string,
) (*types.EnterpriseReadSnapshot, error) {
	if kb == "selected" {
		return nil, types.ErrLegacySourceRestricted
	}
	snapshot, err := s.selectedMCPStore.LoadDocumentReadSnapshot(ctx, user, key, "selected")
	if err != nil {
		return nil, err
	}
	snapshot.KBID = kb
	snapshot.Documents = nil
	return snapshot, nil
}
