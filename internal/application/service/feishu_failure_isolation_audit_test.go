package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Regressions for independent platform reads after Feishu connector failures.
func TestAuditFeishuLiveReceiptRoutesUnrelatedIMSearchIntoViewerGate(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	ctx := types.WithLocalSourceContentRead(types.WithPrincipal(types.WithCaller(f.userContext("A"), types.Caller{
		TenantID: 1, UserID: "system-1", Role: types.TenantRoleViewer,
	}), types.Principal{Type: types.PrincipalIMUser, ID: "im:feishu:external"}))
	s := &knowledgeBaseService{personalRecovery: f.p}
	params := types.SearchParams{KnowledgeBaseIDs: []string{"kb"}}
	handled, _, err := s.exactRecoveryHybridSearch(ctx, "kb", params)
	require.NoError(t, err)
	require.False(t, handled)
	require.NoError(t, f.db.Create(&types.Knowledge{
		ID: "personal", TenantID: 1, KnowledgeBaseID: "kb", Channel: "feishu_personal",
		ParseStatus: types.ParseStatusFailed, EnableStatus: "enabled",
	}).Error)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		TenantID: 1, Scope: types.PersonalExactReceiptScope, ScopeID: "personal", Op: "failed",
		Payload: json.RawMessage(`{"knowledge_base_id":"kb"}`),
	}).Error)
	handled, _, err = s.exactRecoveryHybridSearch(ctx, "kb", params)
	require.False(t, handled)
	require.NoError(t, err, "a failed unrelated personal file cannot intercept platform IM search")
}

func TestAuditFeishuMixedSearchOmitsHealthyConnectorDocuments(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	ctx := types.WithLocalSourceContentRead(types.WithPrincipal(types.WithCaller(f.userContext("A"), types.Caller{
		TenantID: 1, UserID: "A", Role: types.TenantRoleAdmin,
	}), types.Principal{Type: types.PrincipalWebUser, ID: "A"}))
	for _, pair := range [][2]string{{"personal", "feishu_personal"}, {"healthy-sync", "feishu"}, {"ordinary", "web"}} {
		require.NoError(t, f.db.Create(&types.Knowledge{
			ID: pair[0], TenantID: 1, KnowledgeBaseID: "kb", Channel: pair[1],
			ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
		}).Error)
	}
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		TenantID: 1, Scope: types.PersonalExactReceiptScope, ScopeID: "personal", Op: "published",
		Payload: json.RawMessage(`{"knowledge_base_id":"kb"}`),
	}).Error)
	s := &knowledgeBaseService{personalRecovery: f.p}
	for _, params := range []types.SearchParams{
		{KnowledgeBaseIDs: []string{"kb"}},
		{KnowledgeBaseIDs: []string{"kb", "another-kb"}},
		{KnowledgeIDs: []string{"healthy-sync", "personal"}},
	} {
		handled, _, err := s.exactRecoveryHybridSearch(ctx, "kb", params)
		require.NoError(t, err)
		require.False(t, handled,
			"platform queries must retain the ordinary pipeline including all authorized channels")
	}
}

func TestAuditFeishuPersonalReceiptRejectsWorkspaceMCPPrincipal(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	ctx := types.WithLocalSourceContentRead(types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{
		TenantID: 1, UserID: "system-1", Role: types.TenantRoleViewer,
	}), types.Principal{Type: types.PrincipalMCPEndpoint, ID: "endpoint"}))
	ks := &knowledgeService{personalRecovery: f.p}
	read, handled, err := ks.exactPublicReadContext(ctx, "personal")
	require.NoError(t, err)
	require.False(t, handled)
	require.Equal(t, ctx, read)
	kb := &knowledgeBaseService{personalRecovery: f.p}
	read, err = kb.exactSearchContext(ctx, "kb", types.SearchParams{KnowledgeIDs: []string{"personal"}})
	require.NoError(t, err)
	require.Equal(t, ctx, read)
	bundle, err := f.p.preparePublishedReadBundle(ctx, []*types.SearchResult{
		{KnowledgeID: "personal", KnowledgeBaseID: "kb"},
	})
	require.NoError(t, err)
	require.Nil(t, bundle)
	model := &modelService{personalRecovery: f.p}
	read, err = model.AuthorizeExactPersonalRead(ctx, []*types.SearchResult{
		{KnowledgeID: "personal", KnowledgeBaseID: "kb"},
	})
	require.NoError(t, err)
	require.Equal(t, ctx, read)
}

func TestAuditFeishuDeletedSourceBlocksUnrelatedPublishedWiki(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "false")
	ctx := types.WithLocalSourceContentRead(context.Background())
	pages := repository.NewWikiPageRepository(f.DB)
	work, err := repository.BeginWikiMCPGeneration(ctx, pages, "public-two")
	require.NoError(t, err)
	require.NoError(t, pages.Create(work, &types.WikiPage{
		ID: "audit-published-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/audit",
		Title: "audit published page", Content: "ordinary already published content", Version: 1,
		PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished,
		SourceRefs: types.StringArray{"public-two-doc|source"},
	}))
	store := &repository.PersonalWikiStore{DB: f.DB}
	visible, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, visible, 1)
	require.NoError(t, f.DB.Exec(`INSERT INTO data_sources
(id,tenant_id,knowledge_base_id,name,type,config,status,legacy_access_state,deleted_at)
VALUES('deleted-source',1,'public-two','audit','feishu','{}','paused','revoked',CURRENT_TIMESTAMP)`).Error)
	visible, _, err = store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, visible, 1)
	require.NoError(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, visible))
	_, err = repository.BeginWikiMCPGeneration(ctx, pages, "public-two")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "published read policy cannot authorize generation")
	restricted, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"another-doc"})
	require.NoError(t, err)
	require.Empty(t, restricted, "personal MCP document scope still constrains Wiki provenance")
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", []string{"another-doc"}, visible))
	require.NoError(t, f.DB.Exec(
		"UPDATE chunks SET content='changed without timestamp' WHERE knowledge_id='public-two-doc'").Error)
	stale, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Empty(t, stale, "actual page input changes invalidate only the affected publication")
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, visible))
}

func TestAuditFeishuLocalPolicyPreservesExplicitDocumentScope(t *testing.T) {
	ctx := types.WithExecutionTenant(types.WithLocalSourceContentRead(context.Background()), 1)
	read, err := types.WithExactPublishedDocumentRead(ctx, 1, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: "scoped", KBID: "kb",
		Read: func(context.Context) error { return nil }, Commit: func(*gorm.DB) error { return nil },
	})
	require.NoError(t, err)
	require.False(t, ordinaryLocalSourceRead(read))
	ks := &knowledgeService{}
	_, handled, err := ks.exactPublicReadContext(read, "other-document")
	require.True(t, handled)
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	search := &knowledgeBaseService{}
	_, err = search.exactSearchContext(read, "other-kb", types.SearchParams{})
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
}

// Exercise actual personal MCP resolution, repository reads and final key check.
func TestAuditFeishuPersonalMCPReadsAndRevalidatesKey(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	portal := publishedServiceM7(t, f)
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET channel='feishu_personal' WHERE id='public-two-doc'").Error)
	ks := &knowledgeService{repo: portal.Knowledge}
	personal := &FeishuPersonalService{db: f.DB, knowledge: ks}
	recovery := &PersonalRecovery{service: personal}
	ks.personalRecovery = recovery
	portal.personalRecovery = recovery
	ctx := types.WithLocalSourceContentRead(fixture.Key("A", 1, 1, true))
	hits, err := portal.ReadDocument(ctx, "public-two-doc")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "public-two-doc", hits[0].DocumentID)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
	_, err = portal.ReadDocument(ctx, "public-two-doc")
	require.Error(t, err, "platform policy never replaces live API key authorization")
}
