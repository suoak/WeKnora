package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func prepareWikiSelectionFixture(t *testing.T) *fixture.Fixture {
	t.Helper()
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "public-two")
	var f *fixture.Fixture
	if os.Getenv("WIKI_SELECTION_NATIVE_TEST") == "true" {
		dsn := feishulegacy.PrivateDSN(t)
		root, err := filepath.Abs("../../..")
		require.NoError(t, err)
		t.Chdir(root)
		require.NoError(t, database.RunMigrations(dsn))
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, pool.Close())
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			name := strings.TrimPrefix(parsed.Path, "/")
			require.True(t, strings.HasPrefix(name, "feishu_atomic_test_"))
			require.NotContains(t, name, "\"")
			admin, err := gorm.Open(postgres.Open(os.Getenv("FEISHU_LEGACY_TEST_DSN")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, admin.Exec("DROP DATABASE \""+name+"\" WITH (FORCE)").Error)
			p, err := admin.DB()
			require.NoError(t, err)
			require.NoError(t, p.Close())
		})
		f = fixture.Seed(t, db)
	} else {
		f = fixture.New(t)
	}
	f.PreparePublished(t)
	require.NoError(t, f.DB.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	return f
}

func TestWikiDocumentSelectionRetainsSkippedOperationAndRestoresExactRow(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	addWikiSelectionDocument(t, db, "waiting", "web", "failed", "enabled")
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	op := types.TaskPendingOp{
		TenantID:  1,
		Scope:     "knowledge_base",
		ScopeID:   "public-two",
		TaskType:  "wiki:ingest",
		Op:        "ingest",
		DedupKey:  "waiting",
		Payload:   json.RawMessage(`{"op":"ingest","knowledge_id":"waiting","attempt":7}`),
		FailCount: 2,
		ClaimedAt: &stamp,
	}
	require.NoError(t, db.Create(&op).Error)
	ctx, _, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	_, err = repository.ArchiveWikiSkippedOperations(
		ctx,
		repo,
		"public-two",
		[]int64{op.ID},
		map[int64]time.Time{op.ID: stamp.Add(-time.Second)},
	)
	require.Error(t, err, "a stale worker cannot settle another worker's lease")
	ids, err := repository.ArchiveWikiSkippedOperations(
		ctx,
		repo,
		"public-two",
		[]int64{op.ID},
		map[int64]time.Time{op.ID: stamp},
	)
	require.NoError(t, err)
	require.Equal(t, []int64{op.ID}, ids)
	var saved types.TaskPendingOp
	require.NoError(t, db.First(&saved, op.ID).Error)
	require.Equal(t, "wiki_skipped_ops", saved.Scope)
	require.Nil(t, saved.ClaimedAt)
	require.Equal(t, op.FailCount, saved.FailCount)
	require.JSONEq(t, string(op.Payload), string(saved.Payload))
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).Where("id='waiting'").Update("parse_status", "completed").Error,
	)
	_, selection, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	require.True(t, selection.Eligible["waiting"])
	saved = types.TaskPendingOp{}
	require.NoError(t, db.First(&saved, op.ID).Error)
	require.Equal(t, "knowledge_base", saved.Scope)
	require.Equal(t, op.FailCount, saved.FailCount)
	require.JSONEq(t, string(op.Payload), string(saved.Payload))
}

func addWikiSelectionDocument(t *testing.T, db *gorm.DB, id, channel, status, enabled string) {
	t.Helper()
	var doc types.Knowledge
	require.NoError(t, db.First(&doc, "id='public-two-doc'").Error)
	doc.ID = id
	doc.Channel = channel
	doc.ParseStatus = status
	doc.EnableStatus = enabled
	require.NoError(t, db.Create(&doc).Error)
	require.NoError(
		t,
		db.Create(
			&types.Chunk{
				ID:              id + "-chunk",
				TenantID:        1,
				KnowledgeBaseID: "public-two",
				KnowledgeID:     id,
				Content:         "synthetic input",
				ChunkType:       "text",
				IsEnabled:       true,
			},
		).Error,
	)
}

func TestWikiDocumentSelectionSkipsUnrelatedInvalidDocuments(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	strict, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	valid := &types.WikiPage{
		ID:              "valid",
		TenantID:        1,
		KnowledgeBaseID: "public-two",
		Slug:            "concept/valid",
		PageType:        "concept",
		Status:          "published",
		Content:         "verified current body",
		Version:         1,
		SourceRefs:      types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(strict, valid))
	addWikiSelectionDocument(t, db, "missing-proof", "feishu", "completed", "enabled")
	addWikiSelectionDocument(t, db, "failed", "web", "failed", "enabled")
	addWikiSelectionDocument(t, db, "disabled", "web", "completed", "disabled")
	addWikiSelectionDocument(t, db, "personal", "feishu_personal", "completed", "enabled")
	unknown := &types.WikiPage{
		ID:              "unknown",
		TenantID:        1,
		KnowledgeBaseID: "public-two",
		Slug:            "concept/unknown",
		PageType:        "concept",
		Status:          "published",
		Content:         "UNVERIFIED_BODY_MUST_NOT_REACH_MODEL",
		Version:         1,
		SourceRefs:      types.StringArray{"missing-proof"},
	}
	require.NoError(t, db.Create(unknown).Error)
	work, selection, err := repository.BeginWikiDocumentGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"public-two-doc": true}, selection.Eligible)
	require.Len(t, selection.Skipped, 4)
	require.Equal(t, 1, selection.IsolatedPages)
	eligible, selected := repository.WikiSelectedDocument(work, "missing-proof")
	require.True(t, selected)
	require.False(t, eligible)
	page, err := repo.GetBySlug(work, "public-two", "concept/valid")
	require.NoError(t, err)
	require.Equal(t, valid.Content, page.Content)
	_, err = repo.GetBySlug(work, "public-two", "concept/unknown")
	require.Error(t, err)
	var retained types.WikiPage
	require.NoError(t, db.Unscoped().First(&retained, "id='unknown'").Error)
	require.True(t, retained.DeletedAt.Valid)
	require.Equal(t, unknown.Content, retained.Content)
	// Failure of an excluded input cannot invalidate a good sibling's write.
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).Where("id='failed'").Update("description", "changed excluded input").Error,
	)
	newPage := &types.WikiPage{
		ID:              "new",
		TenantID:        1,
		KnowledgeBaseID: "public-two",
		Slug:            "concept/new",
		PageType:        "concept",
		Status:          "published",
		Content:         "verified new content",
		Version:         1,
		SourceRefs:      types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(work, newPage))
	// A selected document mutation still fences this captured generation.
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).
			Where("id='public-two-doc'").
			Update("description", "changed selected input").
			Error,
	)
	newPage.ID = "stale"
	newPage.Slug = "concept/stale"
	require.Error(t, repo.Create(work, newPage))
}

func TestWikiDocumentSelectionSourceRevocationIsLocalAndStillFenced(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config) VALUES('selected-source',1,'public-two','synthetic','feishu','active','active','{}')`,
		).Error,
	)
	addWikiSelectionDocument(t, db, "feishu-good", "feishu", "completed", "enabled")
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO feishu_legacy_resources(id,tenant_id,knowledge_base_id,data_source_id,external_id,knowledge_id,state,active_attempt) VALUES('selected-resource',1,'public-two','selected-source','synthetic','feishu-good','active','published-attempt')`,
		).Error,
	)
	work, selection, err := repository.BeginWikiDocumentGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	require.True(t, selection.Eligible["feishu-good"])
	require.NoError(
		t,
		db.Exec("UPDATE data_sources SET legacy_access_state='revoked' WHERE id='selected-source'").Error,
	)
	p := &types.WikiPage{
		ID:              "revoked",
		TenantID:        1,
		KnowledgeBaseID: "public-two",
		Slug:            "concept/revoked",
		PageType:        "concept",
		Status:          "published",
		Content:         "must not publish",
		Version:         1,
		SourceRefs:      types.StringArray{"feishu-good"},
	}
	require.Error(t, repo.Create(work, p))
	fresh, selection, err := repository.BeginWikiDocumentGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	require.False(t, selection.Eligible["feishu-good"])
	require.True(t, selection.Eligible["public-two-doc"])
	p.ID = "unrelated"
	p.Slug = "concept/unrelated"
	p.SourceRefs = types.StringArray{"public-two-doc"}
	require.NoError(t, repo.Create(fresh, p))
}

func TestWikiDocumentSelectionNoEligibleDocumentsRetainsReason(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("enable_status", "disabled").Error,
	)
	_, selection, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.ErrorIs(t, err, repository.ErrWikiNoEligibleDocuments)
	require.Equal(t, "document_disabled", selection.Skipped["public-two-doc"])
	var records int64
	require.NoError(
		t,
		db.Model(&types.TaskPendingOp{}).
			Where("scope='wiki_document_skip' AND op='skipped'").
			Count(&records).
			Error,
	)
	require.EqualValues(t, 1, records)
	_, _, err = repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.ErrorIs(t, err, repository.ErrWikiNoEligibleDocuments)
	require.NoError(
		t,
		db.Model(&types.TaskPendingOp{}).Where("scope='wiki_document_skip'").Count(&records).Error,
	)
	require.EqualValues(t, 1, records, "retries update the same diagnostic record")
}

func TestWikiDocumentSelectionRecheckRestoresAfterPublicationWithoutBusyLoop(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	addWikiSelectionDocument(t, db, "recoverable", "web", "failed", "enabled")
	op := types.TaskPendingOp{TenantID: 1, Scope: "knowledge_base", ScopeID: "public-two", TaskType: "wiki:ingest", Op: "ingest", DedupKey: "recoverable", Payload: json.RawMessage(`{"op":"ingest","knowledge_id":"recoverable","attempt":3}`)}
	require.NoError(t, db.Create(&op).Error)
	work, _, err := repository.BeginWikiDocumentGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	_, err = repository.ArchiveWikiSkippedOperations(work, repo, "public-two", []int64{op.ID}, nil)
	require.NoError(t, err)
	targets, err := repository.RecheckWikiSkippedOperations(ctx, db, 32)
	require.NoError(t, err)
	require.Empty(t, targets, "fresh skips are not polled repeatedly")
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='recoverable'").Update("parse_status", "completed").Error)
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("scope='wiki_document_skip' AND dedup_key='recoverable'").Update("enqueued_at", time.Now().UTC().Add(-time.Hour)).Error)
	targets, err = repository.RecheckWikiSkippedOperations(ctx, db, 32)
	require.NoError(t, err)
	require.Equal(t, []repository.WikiSkipRecheckTarget{{TenantID: 1, KnowledgeBaseID: "public-two"}}, targets)
	var saved types.TaskPendingOp
	require.NoError(t, db.First(&saved, op.ID).Error)
	require.Equal(t, "knowledge_base", saved.Scope)
	require.JSONEq(t, string(op.Payload), string(saved.Payload))
}

func TestWikiDocumentSelectionRejectsMismatchedPendingIdentity(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	op := types.TaskPendingOp{TenantID: 1, Scope: "knowledge_base", ScopeID: "public-two", TaskType: "wiki:ingest", Op: "ingest", DedupKey: "public-two-doc", Payload: json.RawMessage(`{"op":"ingest","knowledge_id":"unproved-other-document"}`)}
	require.NoError(t, db.Create(&op).Error)
	ctx, _, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	ids, err := repository.ArchiveWikiSkippedOperations(ctx, repo, "public-two", []int64{op.ID}, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{op.ID}, ids)
	var saved types.TaskPendingOp
	require.NoError(t, db.First(&saved, op.ID).Error)
	require.Equal(t, "wiki_regeneration_archive", saved.Scope)
	require.JSONEq(t, string(op.Payload), string(saved.Payload))
}

func TestWikiDocumentSelectionPageReaderUsesSealedInputs(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx, _, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{ID: "readable", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/readable", PageType: "concept", Status: "published", Content: "verified page", Version: 1, SourceRefs: types.StringArray{"public-two-doc"}}
	require.NoError(t, repo.Create(ctx, page))
	addWikiSelectionDocument(t, db, "unrelated-unproved", "feishu", "completed", "enabled")
	store := &repository.PersonalWikiStore{DB: db}
	pages, _, err := store.VerifiedPages(context.Background(), 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.NoError(t, store.RevalidatePages(context.Background(), 1, "public-two", []string{"public-two-doc"}, pages))
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("enable_status", "disabled").Error)
	require.Error(t, store.RevalidatePages(context.Background(), 1, "public-two", []string{"public-two-doc"}, pages))
	pages, partial, err := store.VerifiedPages(context.Background(), 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.True(t, partial)
	require.Empty(t, pages)
}

func TestWikiDocumentSelectionProductionReadGuardsDoNotBlockGoodSibling(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	f := prepareWikiSelectionFixture(t)
	addWikiSelectionDocument(t, f.DB, "held-document", "feishu", "completed", "enabled")
	db := types.EnableLegacyReadGuards(f.DB)
	repo := repository.NewWikiPageRepository(db)
	// This is the production plugin, including its pre-query Wiki predicate.
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config) VALUES('held-source',1,'public-two','synthetic','feishu','active','hold','{}')`).Error)
	ctx, selected, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	require.True(t, selected.Eligible["public-two-doc"])
	require.Contains(t, selected.Skipped, "held-document")
	var good types.Knowledge
	require.NoError(t, db.WithContext(ctx).First(&good, "id='public-two-doc'").Error)
	var denied types.Knowledge
	require.Error(t, db.WithContext(ctx).First(&denied, "id='held-document'").Error)
	p := &types.WikiPage{ID: "production-guard-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/good", PageType: "concept", Status: "published", Content: "current verified content", Version: 1, SourceRefs: types.StringArray{"public-two-doc"}}
	require.NoError(t, repo.Create(ctx, p))
	read, err := repo.GetBySlug(ctx, "public-two", p.Slug)
	require.NoError(t, err)
	require.Equal(t, p.Content, read.Content)
	store := &repository.PersonalWikiStore{DB: db}
	pages, _, err := store.VerifiedPages(context.Background(), 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.NoError(t, store.RevalidatePages(context.Background(), 1, "public-two", []string{"public-two-doc"}, pages))
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("enable_status", "disabled").Error)
	require.Error(t, db.WithContext(ctx).First(&good, "id='public-two-doc'").Error)
}

func TestWikiDocumentSelectionPreservesNewPublicationBesideParkedOperation(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	old := types.TaskPendingOp{TenantID: 1, Scope: "wiki_skipped_ops", ScopeID: "public-two", TaskType: "wiki:ingest", Op: "ingest", DedupKey: "public-two-doc", Payload: json.RawMessage(`{"op":"ingest","knowledge_id":"public-two-doc","attempt":1}`)}
	require.NoError(t, db.Create(&old).Error)
	live := old
	live.ID = 0
	live.Scope = "knowledge_base"
	live.Payload = json.RawMessage(`{"op":"ingest","knowledge_id":"public-two-doc","attempt":2}`)
	require.NoError(t, db.Create(&live).Error)
	_, _, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err, "a newer live operation cannot cause restoration to abort")
	var saved types.TaskPendingOp
	require.NoError(t, db.First(&saved, old.ID).Error)
	require.Equal(t, "wiki_skipped_ops", saved.Scope)
	require.JSONEq(t, string(old.Payload), string(saved.Payload))
	saved = types.TaskPendingOp{}
	require.NoError(t, db.First(&saved, live.ID).Error)
	require.JSONEq(t, string(live.Payload), string(saved.Payload))
	require.NoError(t, db.Delete(&types.TaskPendingOp{}, live.ID).Error)
	_, _, err = repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	saved = types.TaskPendingOp{}
	require.NoError(t, db.First(&saved, old.ID).Error)
	require.Equal(t, "knowledge_base", saved.Scope)
	require.JSONEq(t, string(old.Payload), string(saved.Payload))
}

func TestWikiDocumentSelectionPreviewDoesNotModifyPagesOrQueue(t *testing.T) {
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	addWikiSelectionDocument(t, db, "preview-blocked", "feishu", "completed", "enabled")
	page := &types.WikiPage{ID: "preview-unknown", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/old", PageType: "concept", Status: "published", Content: "historical bytes", Version: 1, SourceRefs: types.StringArray{"public-two-doc", "preview-blocked"}}
	require.NoError(t, db.Create(page).Error)
	var before int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&before).Error)
	plan, err := repository.PreviewWikiDocumentSelection(context.Background(), db, 1, "public-two")
	require.NoError(t, err)
	require.Equal(t, []string{"public-two-doc"}, plan.EligibleDocuments)
	require.Equal(t, "source_verification_required", plan.SkippedDocuments["preview-blocked"])
	require.Equal(t, []string{page.ID}, plan.IsolatedPages)
	require.NotEmpty(t, plan.Snapshot)
	var saved types.WikiPage
	require.NoError(t, db.First(&saved, "id=?", page.ID).Error)
	require.Equal(t, page.Slug, saved.Slug)
	require.Equal(t, page.Content, saved.Content)
	require.False(t, saved.DeletedAt.Valid)
	var after int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&after).Error)
	require.Equal(t, before, after)
	_, err = repository.PreviewWikiDocumentSelection(context.Background(), db, 2, "public-two")
	require.Error(t, err)
}

func TestWikiDocumentSelectionBrowserKeepsGoodPagesReadableAndFencesOutput(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	f := prepareWikiSelectionFixture(t)
	db := types.EnableLegacyReadGuards(f.DB)
	repo := repository.NewWikiPageRepository(db)
	work, _, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	p := &types.WikiPage{ID: "browser-good", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/browser", PageType: "concept", Status: "published", Content: "verified browser body", Version: 1, SourceRefs: types.StringArray{"public-two-doc"}}
	require.NoError(t, repo.Create(work, p))
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config) VALUES('browser-held-source',1,'public-two','synthetic','feishu','active','hold','{}')`).Error)
	unknown := *p
	unknown.ID = "browser-unknown"
	unknown.Slug = "concept/unknown"
	unknown.Content = "unknown old body"
	require.NoError(t, db.Create(&unknown).Error)
	web := types.WithLegacyReadScope(fixture.Web("A", 1))
	read, err := repository.BeginVerifiedWikiBrowserRead(web, repo, "public-two")
	require.NoError(t, err)
	pages, err := repo.ListAll(read, "public-two")
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, p.Content, pages[0].Content)
	require.True(t, types.LegacyReadPartial(read))
	entries, total, err := repo.ListByTypeLight(read, "public-two", "concept", 20, 0)
	require.NoError(t, err, "historical JSON null category paths must not break the index")
	require.Len(t, entries, 1)
	require.EqualValues(t, 1, total)
	stats, err := (&wikiPageService{repo: repo}).GetStats(read, "public-two")
	require.NoError(t, err, "nested read guards must not inherit the caller's COUNT destination")
	require.EqualValues(t, 1, stats.TotalPages)
	require.NoError(t, types.CheckLegacyReadScope(db.WithContext(read)))
	var raw types.Knowledge
	require.Error(t, db.WithContext(read).First(&raw, "id='public-two-doc'").Error, "page scope does not authorize a raw document body")
	_, err = repository.BeginVerifiedWikiBrowserRead(fixture.Key("A", 1, 1, true), repo, "public-two")
	require.Error(t, err, "API keys cannot obtain a workspace browser capability")
	require.NoError(t, db.Model(&types.WikiPage{}).Where("id=?", p.ID).Update("content", "raw changed after read").Error)
	require.Error(t, types.CheckLegacyReadScope(db.WithContext(read)), "final writer rejects concurrent raw page edits")
}
