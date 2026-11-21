package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
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
	store.held = true
	partial, err := restricted.knowledge.SearchDocuments(ctx, []string{"selected"}, "PUBLIC", 20)
	require.NoError(t, err)
	require.Equal(t, "partial", partial.Status)
	require.Empty(t, partial.Documents)
}
