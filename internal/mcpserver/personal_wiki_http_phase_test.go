package mcpserver

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type httpPhaseWiki struct {
	beforeReturn func()
}

func (w *httpPhaseWiki) VerifiedPages(
	_ context.Context, tenant uint64, kb string, _ []string,
) ([]*types.WikiPage, bool, error) {
	w.beforeReturn()
	return []*types.WikiPage{{
		TenantID: tenant, KnowledgeBaseID: kb, Slug: "concept/synthetic",
		Title: "synthetic PRIVATE_WIKI_RESULT", Content: "synthetic candidate",
	}}, false, nil
}

func (*httpPhaseWiki) RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error {
	return nil
}

func TestPersonalWikiHTTPAdmissionSharesPolicyWithinIndependentPhases(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "revoke"}[revoke], func(t *testing.T) {
			s, original, ctx := selectedMCPFixture()
			store := &admissionSnapshotStore{selectedMCPStore: original}
			s.knowledge.Resolver.Store = store
			s.knowledge.Resolver.Documents = access.NewDocumentReader(
				true, store, selectedMCPBase{}, selectedMCPEmployee{},
			)
			loadsAtRead := 0
			s.knowledge.Wiki = &httpPhaseWiki{beforeReturn: func() {
				loadsAtRead = store.loads
				original.revoked = revoke
			}}
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wiki_search",` +
				`"arguments":{"query":"synthetic","knowledge_base_ids":["selected"],"regex":false}}}`
			r := httptest.NewRequest("POST", personalPath, bytes.NewBufferString(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			require.Equal(t, 200, w.Code)
			require.Equal(t, 3, loadsAtRead, "HTTP admission, guard and candidate phases each need one load")
			require.Greater(t, store.loads, loadsAtRead, "output must independently reload authority")
			if revoke {
				require.NotContains(t, w.Body.String(), "PRIVATE_WIKI_RESULT")
				require.Contains(t, w.Body.String(), `"isError":true`)
			} else {
				require.Contains(t, w.Body.String(), "PRIVATE_WIKI_RESULT")
			}
		})
	}
}
