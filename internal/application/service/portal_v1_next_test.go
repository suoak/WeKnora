package service

import (
	"context"
	"sort"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type heldFirstPortalDirectory struct{ access.PortalKBDirectory }

func (d heldFirstPortalDirectory) ListPortalMCPCandidates(ctx context.Context) ([]access.PortalKBDescriptor, error) {
	rows, err := d.PortalKBDirectory.ListPortalMCPCandidates(ctx)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID == "public-kb" && rows[j].ID != "public-kb" })
	return rows, err
}

func TestV1NextSourceIsolationAndGlobalFailure(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	s := publishedServiceM7(t, f)
	s.Resolver.Directory = heldFirstPortalDirectory{s.Resolver.Directory}
	ctx, d := access.WithPortalDiagnostics(fixture.Key("A", 1, 1, true))
	require.NoError(t, f.DB.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config,status,legacy_access_state) VALUES('held',99,'public-kb','synthetic','feishu','{}','active','hold')`).Error)
	targets, err := s.Resolve(ctx, nil)
	require.NoError(t, err)
	require.True(t, d.Partial())
	for _, target := range targets {
		require.NotEqual(t, "public-kb", target.Descriptor().ID)
	}
	_, err = s.ReadDocument(ctx, "public-two-doc")
	require.NoError(t, err)
	selected, selectErr := s.Resolve(ctx, []string{"public-kb", "public-two"})
	require.NoError(t, selectErr)
	require.Len(t, selected, 1)
	require.Equal(t, "public-two", selected[0].Descriptor().ID)
	_, selectErr = s.Resolve(ctx, []string{"public-two", "nonexistent"})
	require.Error(t, selectErr)
	_, err = s.ReadDocument(ctx, "public-doc")
	require.Error(t, err)
	_, err = s.Resolve(ctx, []string{"public-kb"})
	require.Error(t, err)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
	_, err = s.Resolve(ctx, nil)
	require.Error(t, err, "source isolation cannot turn a global credential failure into partial success")
}

func TestV1NextDynamicPersonalSpacesKeepHistoricalScope(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	ctx := context.Background()
	keys := repository.NewTenantAPIKeyRepository(f.DB).(interfaces.UserMCPAPIKeyRepository)
	for _, id := range []int{7, 8} {
		require.NoError(t, f.DB.Exec("INSERT INTO tenants(id,name,business)VALUES(?,?,'synthetic')", id, "next-space").Error)
	}
	// Explicitly persist new-mode opt-in for synthetic A and B keys, not old A3.
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET personal_scope_mode='dynamic_all' WHERE id IN (1,6)").Error)
	_, err := keys.GetUserMCPTenantScope(ctx, 1, 7)
	require.Error(t, err)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status)VALUES('A',7,'viewer','active'),('B',8,'viewer','active')").Error)
	scope, err := keys.GetUserMCPTenantScope(ctx, 1, 7)
	require.NoError(t, err)
	require.Equal(t, types.APIKeyKBScopeAll, scope.KBScopeMode)
	_, err = keys.GetUserMCPTenantScope(ctx, 6, 7)
	require.Error(t, err, "B cannot inherit A's private spaces")
	_, err = keys.GetUserMCPTenantScope(ctx, 3, 7)
	require.Error(t, err, "old A key keeps fixed scope")
	_, err = keys.GetUserMCPTenantScope(ctx, 6, 8)
	require.NoError(t, err)
	require.NoError(t, f.DB.Exec("UPDATE tenant_members SET status='inactive' WHERE user_id='A' AND tenant_id=7").Error)
	_, err = keys.GetUserMCPTenantScope(ctx, 1, 7)
	require.Error(t, err)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=6").Error)
	_, err = keys.GetUserMCPTenantScope(ctx, 6, 8)
	require.Error(t, err)
}

func TestV1NextWikiProofReadOnlyAndEpochRevocation(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	r := repository.NewWikiPageRepository(f.DB)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{ID: "next-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/shared", Title: "verified concept", Content: "ordinary linked [source](https://example.test/path)", Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source title"}, Version: 1}
	require.NoError(t, r.Create(ctx, page))
	s := publishedServiceM7(t, f)
	employee := fixture.Key("A", 1, 1, true)
	result, err := s.PersonalWiki(employee, "wiki_search", "verified", "", "", 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	result, err = s.PersonalWiki(employee, "wiki_read_page", "", "public-two", "concept/shared", 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	require.NotContains(t, result.Pages[0].Content, "https://")
	var before, after int64
	require.NoError(t, f.DB.Table("wiki_pages").Count(&before).Error)
	result, err = s.PersonalWiki(employee, "wiki_index", "", "public-kb", "", 50, 0)
	require.NoError(t, err)
	require.Empty(t, result.Pages)
	require.NoError(t, f.DB.Table("wiki_pages").Count(&after).Error)
	require.Equal(t, before, after, "missing index cannot create a default page")
	// No timestamp heuristic: a raw old-worker write preserving updated_at must invalidate proof.
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content='new version' WHERE knowledge_id='public-two-doc'").Error)
	_, err = s.PersonalWiki(employee, "wiki_read_page", "", "public-two", "concept/shared", 50, 0)
	require.Error(t, err)
	result, err = s.PersonalWiki(employee, "wiki_search", "verified", "", "", 50, 0)
	require.NoError(t, err)
	require.Empty(t, result.Pages)
	require.Equal(t, "partial", result.Status)
}

func TestV1NextWikiGenerationFencesInputsAndNormalCompletion(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	r := repository.NewWikiPageRepository(f.DB)
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET parse_status='finalizing' WHERE id='public-two-doc'").Error)
	pages := NewWikiPageService(r, nil, nil, nil, nil, nil).(*wikiPageService)
	ctx, proofErr := pages.personalWikiGenerationContext(context.Background(), "public-two")
	require.NoError(t, proofErr)
	page := &types.WikiPage{ID: "generated", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/generated", Title: "generated", Content: "verified wiki content", Status: types.WikiPageStatusDraft, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1}
	_, e := pages.CreatePage(ctx, page)
	require.NoError(t, e)
	page.Status = types.WikiPageStatusPublished
	require.NoError(t, pages.UpdatePageMeta(ctx, page))
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET parse_status='completed',pending_subtasks_count=0 WHERE id='public-two-doc'").Error)
	s := publishedServiceM7(t, f)
	result, e := s.PersonalWiki(fixture.Key("A", 1, 1, true), "wiki_read_page", "", "public-two", "concept/generated", 50, 0)
	require.NoError(t, e)
	require.Len(t, result.Pages, 1)
	ctx, e = repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, e)
	gate := repository.NewModelRepository(f.DB).(legacyModelInputGate)
	spy := &legacySpyModel{}
	model := &legacyGuardedChat{inner: spy, gate: gate, tenant: 1}
	// A different held KB in the same tenant does not contaminate this complete
	// server-bound generation closure. Unknown/unproven model inputs still deny.
	require.NoError(t, f.DB.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config,status,legacy_access_state) VALUES('held-other',1,'private-home','synthetic','feishu','{}','active','hold')`).Error)
	_, e = model.Chat(ctx, []chat.Message{{Content: "verified wiki input"}}, nil)
	require.NoError(t, e)
	require.EqualValues(t, 1, spy.calls.Load())
	_, e = model.Chat(context.Background(), []chat.Message{{Content: "unknown input closure"}}, nil)
	require.Error(t, e)
	require.EqualValues(t, 1, spy.calls.Load())
	// Raw/old writer insertion followed by deletion must not evade the input
	// fence just because the final set of visible pages looks the same (ABA).
	require.NoError(t, f.DB.Exec(`INSERT INTO wiki_pages(id,tenant_id,knowledge_base_id,slug,title,content,page_type,status) VALUES('transient',1,'public-two','concept/transient','secret','TRANSIENT_CANARY','concept','published')`).Error)
	require.NoError(t, f.DB.Exec("DELETE FROM wiki_pages WHERE id='transient'").Error)
	_, e = model.Chat(ctx, []chat.Message{{Content: "TRANSIENT_CANARY"}}, nil)
	require.Error(t, e)
	require.EqualValues(t, 1, spy.calls.Load(), "unsafe input must not enter a model call")
	page.ID = "late"
	page.Slug = "concept/late"
	require.Error(t, r.Create(ctx, page), "a stale generator cannot certify a late page")
	var count int64
	require.NoError(t, f.DB.Table("wiki_pages").Where("id='late'").Count(&count).Error)
	require.Zero(t, count)
}

func TestV1NextWikiAmbiguityUnknownMixedAndOldWriterABA(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	r := repository.NewWikiPageRepository(f.DB)
	for _, kb := range []string{"public-two", "private-home"} {
		ctx, e := repository.BeginWikiMCPGeneration(context.Background(), r, kb)
		require.NoError(t, e)
		p := &types.WikiPage{ID: "page-" + kb, TenantID: 1, KnowledgeBaseID: kb, Slug: "concept/same", Title: "title-" + kb, Content: "body-" + kb, Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{kb + "-doc|source"}, Version: 1}
		require.NoError(t, r.Create(ctx, p))
	}
	s := publishedServiceM7(t, f)
	a := fixture.Key("A", 1, 1, true)
	result, e := s.PersonalWiki(a, "wiki_read_page", "", "", "concept/same", 50, 0)
	require.NoError(t, e)
	require.Equal(t, "ambiguous", result.Status)
	require.Len(t, result.Pages, 2)
	for _, p := range result.Pages {
		require.Empty(t, p.Content, "ambiguity must not release multiple bodies")
	}
	require.NoError(t, f.DB.Exec(`UPDATE tenant_api_keys SET capabilities='["retrieve","enterprise_retrieve"]' WHERE id=6`).Error)
	result, e = s.PersonalWiki(fixture.Key("B", 99, 6, true), "wiki_read_page", "", "", "concept/same", 50, 0)
	require.NoError(t, e)
	require.Len(t, result.Pages, 1)
	require.Equal(t, "public-two", result.Pages[0].KBID)
	// One permitted citation cannot certify a page mixing an unauthorized source.
	untrusted := &types.WikiPage{ID: "unknown-mixed", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/mixed", Title: "MIXED_CANARY", Content: "MIXED_CANARY", Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|allowed", "sensitive|MIXED_CANARY"}, Version: 1}
	require.NoError(t, r.Create(context.Background(), untrusted))
	result, e = s.PersonalWiki(a, "wiki_search", "CANARY", "public-two", "", 50, 0)
	require.NoError(t, e)
	require.Empty(t, result.Pages)
	require.Equal(t, "partial", result.Status)
	_, e = s.PersonalWiki(a, "wiki_read_page", "", "public-two", "concept/mixed", 50, 0)
	require.Error(t, e)
	require.NoError(t, f.DB.Exec("UPDATE wiki_pages SET content='MIXED_CANARY' WHERE id='page-public-two'").Error)
	require.NoError(t, f.DB.Exec("UPDATE wiki_pages SET content='body-public-two' WHERE id='page-public-two'").Error)
	_, e = s.PersonalWiki(a, "wiki_read_page", "", "public-two", "concept/same", 50, 0)
	require.Error(t, e, "restoring bytes cannot recreate invalidated provenance")
}

func TestV1NextWikiOutputBoundaryRechecksPageArchive(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	r := repository.NewWikiPageRepository(f.DB)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{ID: "archive-boundary", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/boundary", Title: "boundary", Content: "private generated text", Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1}
	require.NoError(t, r.Create(ctx, page))
	store := &repository.PersonalWikiStore{DB: f.DB}
	pages, partial, err := store.VerifiedPages(context.Background(), 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.False(t, partial)
	require.Len(t, pages, 1)
	require.NoError(t, store.RevalidatePages(context.Background(), 1, "public-two", []string{"public-two-doc"}, pages))
	require.NoError(t, f.DB.Exec("UPDATE wiki_pages SET status='archived' WHERE id='archive-boundary'").Error)
	require.Error(t, store.RevalidatePages(context.Background(), 1, "public-two", []string{"public-two-doc"}, pages), "document grant alone cannot authorize an archived page")
}

func TestV1NextWikiGenerationDefaultOffAndUnknownInputsStop(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	r := repository.NewWikiPageRepository(f.DB)
	pages := NewWikiPageService(r, nil, nil, nil, nil, nil)
	settings := &config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: false}}
	worker := &wikiIngestService{knowledgeSvc: &knowledgeService{config: settings}, wikiService: pages}
	original := context.Background()
	ctx, err := worker.personalWikiProofContext(original, "public-two")
	require.NoError(t, err)
	require.Equal(t, original, ctx, "default-off generation preserves the legacy context")
	settings.PersonalMCP.Enabled = true
	require.Error(t, worker.checkPersonalWikiPending([]WikiPendingOp{{Op: "retract", DocSummary: "REVOKED_QUEUE_CANARY"}}))
	require.NoError(t, worker.checkPersonalWikiPending([]WikiPendingOp{{Op: WikiOpIngest, KnowledgeID: "public-two-doc"}}))
	description, refs := worker.personalWikiFinalizeInputs("REVOKED_QUEUE_CANARY", []linkRef{{slug: "concept/old", matchText: "REVOKED_QUEUE_CANARY"}})
	require.NotContains(t, description, "REVOKED_QUEUE_CANARY")
	require.Empty(t, refs)
	ctx, err = worker.personalWikiProofContext(original, "public-two")
	require.NoError(t, err)
	_, proven := repository.WikiMCPGenerationTenant(ctx)
	require.True(t, proven)
	require.NoError(t, r.Create(original, &types.WikiPage{ID: "unknown-input", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/unknown", Title: "unknown", Content: "UNTRUSTED_INPUT_CANARY", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{"sensitive|unknown"}, Version: 1}))
	ctx, err = worker.personalWikiProofContext(original, "public-two")
	require.Error(t, err)
	require.Nil(t, ctx, "unknown inputs cannot continue with an unproved model context")
}
