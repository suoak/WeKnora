package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type selectedReadKBStub struct {
	interfaces.KnowledgeBaseService
	tenant uint64
}

func (s selectedReadKBStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "reviewed", TenantID: s.tenant, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}}, nil
}

type selectedReadWikiStub struct {
	interfaces.WikiPageService
	calls int
}

func (s *selectedReadWikiStub) VerifiedWikiBrowserReadContext(ctx context.Context, _ string) (context.Context, error) {
	s.calls++
	return ctx, nil
}

func TestWikiSelectedBrowserReadKeepsSharesAndKeysOnExistingAuthority(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "reviewed")
	for _, test := range []struct {
		name   string
		tenant uint64
		ctx    context.Context
		calls  int
	}{
		{"same workspace", 1, fixture.Web("A", 1), 1},
		{"cross workspace share", 99, fixture.Web("A", 1), 0},
		{"API key", 1, fixture.Key("A", 1, 7, true), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/wiki", nil).WithContext(test.ctx)
			c.Params = gin.Params{{Key: "kb_id", Value: "reviewed"}}
			c.Set(types.TenantIDContextKey.String(), uint64(1))
			wiki := &selectedReadWikiStub{}
			h := &WikiPageHandler{wikiService: wiki, kbService: selectedReadKBStub{tenant: test.tenant}}
			kb, _, err := h.validateWikiKB(c)
			require.NoError(t, err)
			require.Equal(t, "reviewed", kb)
			require.Equal(t, test.calls, wiki.calls)
		})
	}
}
