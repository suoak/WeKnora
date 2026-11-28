package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalWikiWorkUsesExistingFullScopeBudget(t *testing.T) {
	for _, tool := range []string{"wiki_search", "wiki_read_page", "wiki_index"} {
		require.True(t, personalNeedsSearchWork(tool, nil))
		require.True(t, personalNeedsSearchWork(tool, make([]string, 9)))
		require.False(t, personalNeedsSearchWork(tool, []string{"one-kb"}))
	}
	require.False(t, personalNeedsSearchWork("list_knowledge_bases", nil))
}

func TestPersonalConsistencyFullScopeWorkRejectsBeforeScopeAndPreservesOtherPools(t *testing.T) {
	limits, err := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	require.NoError(t, err)
	s := newPersonalServer(true, &service.PortalMCPKnowledge{}, limits)
	require.Equal(t, limits.GlobalLimit, s.admission.global)
	require.Equal(t, limits.UserLimit, s.admission.perUser)
	s.searchWork.wait = time.Millisecond
	var permits []func()
	for i, user := range []string{"busy-A", "busy-B"} {
		release, err := s.searchWork.acquire(context.Background(), user)
		require.NoError(t, err)
		permits = append(permits, release)
		t.Cleanup(permits[i])
	}
	request := func(name string, ids any, isolated bool) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": map[string]any{
				"query": "safe", "knowledge_base_ids": ids,
			}},
		})
		require.NoError(t, err)
		path, handler := personalPath, s.Handler()
		if isolated {
			path, handler = isolatedPersonalPath, s.IsolatedHandler()
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(types.WithPrincipal(r.Context(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, isolated := range []bool{false, true} {
		for _, tool := range []string{"search_knowledge", "grep_chunks", "search_documents", "wiki_search"} {
			w := request(tool, nil, isolated)
			require.Equal(t, 429, w.Code, "reject overload before attempting the unavailable policy store")
			require.Equal(t, "concurrency_limited", w.Header().Get("X-KnowHub-Error-Code"))
			require.NotEmpty(t, w.Header().Get("Retry-After"))
			require.Zero(t, s.admission.snapshot("owner").Active, "release the ordinary tool permit")
		}
	}
	// Scoped reads still reach authorization while full-scope work is busy.
	require.Equal(t, 403, request("search_knowledge", []string{"one-kb"}, false).Code)
	permits[0]()
	// Authorization failure after acquiring work must return that permit too.
	require.Equal(t, 403, request("search_knowledge", nil, false).Code)
	require.Equal(t, 1, s.searchWork.snapshot("owner").Active)
	require.Zero(t, s.searchWork.snapshot("owner").Queued)
}
