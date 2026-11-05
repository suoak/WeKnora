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
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWikiHistoryRegenerationIsolatesWithoutCertifyingOldText(t *testing.T) {
	f := fixture.New(t)
	testWikiHistoryRegeneration(t, f)
}

func TestWikiHistoryRegenerationResolvesOnlyVerifiedBlock(t *testing.T) {
	testWikiHistoryRegenerationBlock(t, fixture.New(t))
}

func TestWikiHistoryRegenerationDeletedInputEvidence(t *testing.T) {
	testWikiDeletedInputEvidence(t, fixture.New(t))
}

func testWikiDeletedInputEvidence(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db, ctx := f.DB, context.Background()
	require.NoError(t, db.AutoMigrate(
		&types.TaskPendingOp{}, &types.TaskDeadLetter{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	id := "removed-connector-document"
	require.NoError(t, db.Create(&types.Chunk{
		ID: "removed-input", TenantID: 1, KnowledgeBaseID: "public-two", KnowledgeID: id,
		Content: "DELETED_INPUT_NEVER_GENERATE", DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
	}).Error)
	op := types.TaskPendingOp{
		TenantID: 1, Scope: "knowledge_base", ScopeID: "public-two",
		TaskType: "wiki:ingest", Op: "retract", DedupKey: id,
		Payload: json.RawMessage(`{"doc_summary":"HISTORICAL_QUEUE_BODY"}`),
	}
	require.NoError(t, db.Create(&op).Error)
	preview := func() (*repository.WikiRegenerationPlan, error) {
		return repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false, true)
	}
	plan, err := preview()
	require.NoError(t, err)
	require.Equal(t, 1, plan.RetiredRetracts)
	for _, column := range []string{"tenant_id", "knowledge_base_id", "deleted_at"} {
		var invalid, original any
		switch column {
		case "tenant_id":
			invalid, original = 99, 1
		case "knowledge_base_id":
			invalid, original = "private-home", "public-two"
		case "deleted_at":
			invalid = nil
			var chunk types.Chunk
			require.NoError(t, db.Unscoped().First(&chunk, "id='removed-input'").Error)
			original = chunk.DeletedAt.Time
		}
		require.NoError(t, db.Table("chunks").Where("id='removed-input'").Update(column, invalid).Error)
		_, err = preview()
		require.Error(t, err, column)
		require.NoError(t, db.Table("chunks").Where("id='removed-input'").Update(column, original).Error)
	}
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true, true)
	require.ErrorContains(t, err, "snapshot changed",
		"epoch changes invalidate preview even if timestamps are restored")
	_, err = preview()
	require.NoError(t, err)
	var epoch struct {
		KnowledgeID string
		Generation  int64
		Incarnation string
	}
	require.NoError(t, db.Table("document_access_epochs").Where("knowledge_id=?", id).Take(&epoch).Error)
	require.NoError(t, db.Exec("DELETE FROM document_access_epochs WHERE knowledge_id=?", id).Error)
	_, err = preview()
	require.Error(t, err, "missing identity fence cannot establish deletion")
	require.NoError(t, db.Table("document_access_epochs").Create(&epoch).Error)
	// Mere absence, without retained scoped deletion evidence, stays denied.
	require.NoError(t, db.Exec("DELETE FROM chunks WHERE id='removed-input'").Error)
	_, err = preview()
	require.Error(t, err)
	require.NoError(t, db.Create(&types.Chunk{
		ID: "removed-input", TenantID: 1, KnowledgeBaseID: "public-two", KnowledgeID: id,
		Content: "DELETED_INPUT_NEVER_GENERATE", DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
	}).Error)
	plan, err = preview()
	require.NoError(t, err)
	applied, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var retained types.TaskPendingOp
	require.NoError(t, db.First(&retained, op.ID).Error)
	require.Equal(t, "wiki_regeneration_archive", retained.Scope)
	require.Equal(t, string(op.Payload), string(retained.Payload))
	var pending []types.TaskPendingOp
	require.NoError(t, db.Where("scope='knowledge_base'").Find(&pending).Error)
	require.Len(t, pending, 1)
	require.Equal(t, "public-two-doc", pending[0].DedupKey)
	require.NotContains(t, string(pending[0].Payload), "HISTORICAL_QUEUE_BODY")
	require.NotContains(t, string(pending[0].Payload), "DELETED_INPUT_NEVER_GENERATE")
}

func testWikiHistoryRegenerationBlock(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	ctx := context.Background()
	repo := repository.NewWikiPageRepository(db)
	require.NoError(t, repository.WriteWikiProcessingBlock(
		ctx, repo, 1, "public-two", "verified_regeneration_required"))
	require.NoError(t, repository.WriteWikiProcessingBlock(
		ctx, repo, 1, "private-home", "source_verification_required"))
	before, err := repository.ReadWikiProcessingBlock(ctx, repo, "public-two")
	require.NoError(t, err)
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	checkBlocked := func() {
		t.Helper()
		block, readErr := repository.ReadWikiProcessingBlock(ctx, repo, "public-two")
		require.NoError(t, readErr)
		require.Equal(t, before, block)
	}
	checkBlocked()
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "stale-snapshot", true)
	require.ErrorContains(t, err, "snapshot changed")
	checkBlocked()
	_, err = repository.PrepareWikiRegeneration(ctx, db, 99, "public-two", plan.Snapshot, true)
	require.Error(t, err)
	checkBlocked()
	applied, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	block, err := repository.ReadWikiProcessingBlock(ctx, repo, "public-two")
	require.NoError(t, err)
	require.Empty(t, block.Reason)
	require.True(t, block.RetryAfter.IsZero())
	other, err := repository.ReadWikiProcessingBlock(ctx, repo, "private-home")
	require.NoError(t, err)
	require.Equal(t, "source_verification_required", other.Reason)
}

func testWikiHistoryRegeneration(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	old := &types.WikiPage{
		ID: "old", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "index",
		PageType: "index", Status: "published", Content: "UNVERIFIED_HISTORY_CANARY", Version: 9,
	}
	require.NoError(t, db.Create(old).Error)
	_, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.ErrorIs(t, err, repository.ErrWikiMCPUnverified)
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.False(t, plan.Applied)
	require.Equal(t, 1, plan.Documents)
	require.Equal(t, 1, plan.HistoricalPages)
	var n int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&n).Error)
	require.Zero(t, n)
	applied, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var preserved types.WikiPage
	require.NoError(t, db.Unscoped().First(&preserved, "id='old'").Error)
	require.True(t, preserved.DeletedAt.Valid)
	require.Equal(t, old.Content, preserved.Content)
	require.NotEqual(t, "index", preserved.Slug)
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id='old'").Count(&n).Error)
	require.Zero(t, n, "old text must never receive a retrospective proof")
	var op types.TaskPendingOp
	require.NoError(t, db.First(&op).Error)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(op.Payload, &payload))
	require.Equal(t, "public-two-doc", payload["knowledge_id"])
	require.NotContains(t, string(op.Payload), "UNVERIFIED_HISTORY_CANARY")
	proof, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err, "empty generation can now start from current documents")
	newPage := &types.WikiPage{
		ID: "new", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/new",
		PageType: "concept", Status: "published", Content: "newly generated", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, newPage))
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id='new'").Count(&n).Error)
	require.EqualValues(t, 1, n)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.Error(t, err, "apply must not duplicate operations")
}

func TestWikiHistoryRegenerationRejectsChangedAndDeniedSources(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	ctx := context.Background()
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE chunks SET content='changed input' WHERE knowledge_id='public-two-doc'").Error)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.ErrorContains(t, err, "snapshot changed")
	_, err = repository.PrepareWikiRegeneration(ctx, db, 99, "public-two", "", false)
	require.Error(t, err)
	require.NoError(t, db.Exec("UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'").Error)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestWikiHistoryRegenerationResumesOnlyUnclaimedWikiSlots(t *testing.T) {
	for _, change := range []string{
		"valid", "claim", "other_subtask", "changed_input", "disabled_source", "deleted_retract", "foreign_retract",
		"finalize_slug", "finalize_change", "finalize_folder_prune", "claimed_finalize", "changed_finalize",
		"foreign_finalize", "unknown_finalize",
	} {
		t.Run(change, func(t *testing.T) {
			f := wikiPendingRegenerationFixture(t)
			f.PreparePublished(t)
			db, ctx := f.DB, context.Background()
			require.NoError(t, db.AutoMigrate(
				&types.TaskPendingOp{}, &types.TaskDeadLetter{}, &types.KnowledgeProcessingSpan{}))
			require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
				Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Updates(map[string]any{
				"parse_status": "finalizing", "summary_status": "completed", "pending_subtasks_count": 1,
			}).Error)
			old := types.TaskPendingOp{
				TenantID: 1, TaskType: "wiki:ingest", Scope: "knowledge_base",
				ScopeID: "public-two", DedupKey: "public-two-doc", Op: "ingest",
				Payload: json.RawMessage(`{"doc_summary":"UNVERIFIED_QUEUED_TEXT"}`),
			}
			require.NoError(t, db.Create(&old).Error)
			var finalOp types.TaskPendingOp
			if strings.Contains(change, "finalize") {
				op := "slug"
				switch change {
				case "finalize_change":
					op = "change"
				case "finalize_folder_prune":
					op = "folder_prune"
				case "unknown_finalize":
					op = "unknown"
				}
				finalOp = types.TaskPendingOp{
					TenantID: 1, TaskType: "wiki:finalize", Scope: "knowledge_base", ScopeID: "public-two",
					Op: op, DedupKey: "", Payload: json.RawMessage(`{"old":"UNPROVED_FINALIZE_BODY"}`),
				}
				if change == "foreign_finalize" {
					finalOp.TenantID = 99
				}
				require.NoError(t, db.Create(&finalOp).Error)
			}
			if change == "deleted_retract" || change == "foreign_retract" {
				id := "public-one-doc"
				if change == "deleted_retract" {
					id = "removed-document"
					require.NoError(t, db.Create(&types.Knowledge{
						ID: id, TenantID: 1, KnowledgeBaseID: "public-two", Channel: "web",
						Type: "document", ParseStatus: "completed", EnableStatus: "enabled",
						DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
					}).Error)
				}
				require.NoError(t, db.Create(&types.TaskPendingOp{
					TenantID: 1, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: "public-two",
					DedupKey: id, Op: "retract", Payload: json.RawMessage(`{"doc_summary":"OLD_DELETED_BODY"}`),
				}).Error)
			}
			_, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
			require.Error(t, err, "ordinary regeneration cannot consume pending operations")
			plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false, true)
			if change == "foreign_retract" || change == "foreign_finalize" || change == "unknown_finalize" {
				require.Error(t, err)
				var count int64
				require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&count).Error)
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, plan.ResumedPending)
			if finalOp.ID != 0 {
				require.Equal(t, 1, plan.RetiredFinalizes)
			}
			switch change {
			case "claimed_finalize":
				require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("id=?", finalOp.ID).
					Update("claimed_at", time.Now()).Error)
			case "changed_finalize":
				require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("id=?", finalOp.ID).
					Update("payload", json.RawMessage(`{"changed":true}`)).Error)
			case "claim":
				require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("id=?", old.ID).
					Update("claimed_at", time.Now()).Error)
			case "other_subtask":
				require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").
					Update("pending_subtasks_count", 2).Error)
			case "changed_input":
				require.NoError(t, db.Exec(
					"UPDATE chunks SET content='changed' WHERE knowledge_id='public-two-doc'").Error)
			case "disabled_source":
				require.NoError(t, db.Exec(
					"UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'").Error)
			}
			applied, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true, true)
			var count int64
			if change != "valid" && change != "deleted_retract" && !strings.HasPrefix(change, "finalize_") {
				require.Error(t, err)
				require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&count).Error)
				require.Zero(t, count, "rejection must not retire any operation")
				return
			}
			require.NoError(t, err)
			require.True(t, applied.Applied)
			if finalOp.ID != 0 {
				var retained types.TaskPendingOp
				require.NoError(t, db.First(&retained, finalOp.ID).Error)
				require.Equal(t, "wiki_regeneration_archive", retained.Scope)
				require.Equal(t, string(finalOp.Payload), string(retained.Payload))
			}
			var pending types.TaskPendingOp
			require.NoError(t, db.Where("scope='knowledge_base'").First(&pending).Error)
			require.NotEqual(t, old.ID, pending.ID)
			require.NotContains(t, string(pending.Payload), "UNVERIFIED_QUEUED_TEXT")
			var archived types.TaskDeadLetter
			require.NoError(t, db.First(&archived).Error)
			require.NotContains(t, string(archived.Payload), "UNVERIFIED_QUEUED_TEXT")
			require.Equal(t, "wiki:regeneration_archive", archived.TaskType)
			var original types.TaskPendingOp
			require.NoError(t, db.Where("id=?", old.ID).First(&original).Error)
			require.Equal(t, "wiki_regeneration_archive", original.Scope)
			require.Equal(t, string(old.Payload), string(original.Payload))
			if change == "deleted_retract" {
				require.Equal(t, 1, applied.RetiredRetracts)
			}
			var doc types.Knowledge
			require.NoError(t, db.Where("id='public-two-doc'").First(&doc).Error)
			require.Equal(t, "finalizing", doc.ParseStatus)
			require.Equal(t, 1, doc.PendingSubtasksCount, "reuse the one Wiki slot, never increment it twice")
			_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true, true)
			require.Error(t, err, "the same snapshot cannot be applied twice")
		})
	}
}

func wikiPendingRegenerationFixture(t *testing.T) *fixture.Fixture {
	t.Helper()
	dsn := os.Getenv("WIKI_HISTORY_POSTGRES_DSN")
	if dsn == "" {
		return fixture.New(t)
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	name := "wiki_pending_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE DATABASE "+name).Error)
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error) })
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(cwd, "../../..")))
	err = database.RunMigrations(parsed.String())
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return fixture.Seed(t, db)
}

func TestWikiSnapshotGenerationMergesOnlyBacklinks(t *testing.T) {
	f := fixture.New(t)
	testWikiSnapshotGeneration(t, f)
}

func testWikiSnapshotGeneration(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{
		ID: "snapshot-target", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "concept/target", Title: "target", PageType: "concept", Status: "published", Content: "initial",
		SourceRefs: types.StringArray{"public-two-doc"}, Version: 1,
	}
	require.NoError(t, repo.Create(ctx, page))
	svc := &wikiPageService{repo: repo}
	stale, err := svc.GetPageBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	backlinks := repo.(interface {
		UpdateBacklink(context.Context, string, string, string, bool) error
	})
	require.NoError(t, backlinks.UpdateBacklink(ctx, "public-two", page.Slug, "concept/other", true))
	// Public callers still conflict when ANY persisted dependency changed.
	stale.Content = "new model body"
	_, err = svc.UpdatePage(context.Background(), stale)
	require.ErrorIs(t, err, repository.ErrWikiPageConflict)
	updated, err := svc.UpdatePage(ctx, stale)
	require.NoError(t, err)
	require.Equal(t, "new model body", updated.Content)
	require.Contains(t, updated.InLinks, "concept/other")
	// A real body change must never be merged, even with a generation proof.
	stale = updated
	fresh, err := svc.GetPageBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	fresh.Content = "intervening writer"
	_, err = svc.UpdatePage(ctx, fresh)
	require.NoError(t, err)
	stale.Content = "stale overwrite"
	_, err = svc.UpdatePage(ctx, stale)
	require.ErrorIs(t, err, repository.ErrWikiPageConflict)
}

func TestWikiHistoryPostgres(t *testing.T) {
	dsn := os.Getenv("WIKI_HISTORY_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated Wiki PostgreSQL fixture not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := admin.DB()
	require.NoError(t, err)
	defer func() { require.NoError(t, pool.Close()) }()
	for _, tc := range []struct {
		name string
		run  func(*testing.T, *fixture.Fixture)
	}{
		{"regeneration", testWikiHistoryRegeneration},
		{"selective_page_recovery", testWikiSelectiveRecovery},
		{"source_only_recovery", testWikiSelectiveRecoverySourceOnly},
		{"regeneration_block", testWikiHistoryRegenerationBlock},
		{"deleted_input_evidence", testWikiDeletedInputEvidence},
		{"unused_source_draft", testFeishuUnusedDraftAdmission},
		{"feishu_completed_slots", testWikiFeishuIndependentRegeneration},
		{"orphan_retraction", testWikiOrphanRetraction},
		{"snapshot", testWikiSnapshotGeneration},
		{"gitlab_recovery", testWikiProductionGitLab},
		{"verified_empty_gitlab_input", testWikiVerifiedEmptyGitLabInput},
		{"empty_retraction", testWikiEmptyRetractionRetirement},
		{"hidden_retraction_reference", testWikiEmptyRetractionHiddenReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "wiki_history_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			require.NoError(t, admin.Exec("CREATE DATABASE "+name).Error)
			t.Cleanup(func() { require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error) })
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			parsed.Path = "/" + name
			cwd, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(filepath.Join(cwd, "../../..")))
			err = database.RunMigrations(parsed.String())
			require.NoError(t, os.Chdir(cwd))
			require.NoError(t, err)
			db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			tc.run(t, fixture.Seed(t, db))
		})
	}
}

func testWikiFeishuIndependentRegeneration(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	source, resource, attempt := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Create(&types.DataSource{
		ID: source, TenantID: 1, KnowledgeBaseID: "public-two", Type: "feishu",
		Status: "active", LegacyAccessState: "active",
	}).Error)
	metadata, err := json.Marshal(map[string]string{"datasource_id": source, "external_id": "synthetic-doc"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Updates(map[string]any{
		"channel": "feishu", "metadata": types.JSON(metadata),
	}).Error)
	require.NoError(t, db.Create(&types.FeishuLegacyResource{
		ID: resource, TenantID: 1, KnowledgeBaseID: "public-two", DataSourceID: source,
		ExternalID: "synthetic-doc", KnowledgeID: "public-two-doc", State: "active",
		Generation: 1, CurrentAttempt: attempt, ActiveAttempt: attempt,
		QuarantineSnapshot: json.RawMessage(`{}`),
	}).Error)
	require.NoError(t, db.Create(&types.FeishuLegacyAttempt{
		ID: attempt, ResourceID: resource, Generation: 1, Status: "published",
		Manifest: json.RawMessage(`{}`), Receipt: json.RawMessage(`{}`),
	}).Error)
	var before string
	require.NoError(t, db.Raw("SELECT to_jsonb(k)::text FROM knowledges k WHERE id='public-two-doc'").
		Scan(&before).Error)
	// The real migrated trigger must reject arbitrary writes to the published row.
	require.ErrorContains(t, db.Exec("UPDATE knowledges SET title='forbidden' WHERE id='public-two-doc'").Error,
		"stale or unmanaged Feishu source write")
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	count, promoted, err := repository.NewKnowledgeRepository(db).FinalizeSubtask(ctx, "public-two-doc")
	require.NoError(t, err)
	require.Zero(t, count)
	require.False(t, promoted)
	var after string
	require.NoError(t, db.Raw("SELECT to_jsonb(k)::text FROM knowledges k WHERE id='public-two-doc'").
		Scan(&after).Error)
	require.Equal(t, before, after, "Wiki regeneration must preserve the entire published source row")
	var queued int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("task_type='wiki:ingest'").Count(&queued).Error)
	require.EqualValues(t, 1, queued)
	require.NoError(t, db.Model(&types.FeishuLegacyResource{}).Where("id=?", resource).Update("state", "hold").Error)
	_, err = repository.BeginWikiMCPGeneration(ctx, repository.NewWikiPageRepository(db), "public-two")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}
