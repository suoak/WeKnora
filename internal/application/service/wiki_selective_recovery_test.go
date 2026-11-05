package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiSelectiveRecoveryPreservesValidPagesAndOriginalOperations(t *testing.T) {
	testWikiSelectiveRecovery(t, fixture.New(t))
}

func testWikiSelectiveRecovery(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db, ctx := f.DB, context.Background()
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	repo := repository.NewWikiPageRepository(db)
	proof, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	valid := &types.WikiPage{
		ID: "valid", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/valid",
		PageType: "concept", Status: "published", Content: "verified content", Version: 3,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, valid))
	old := &types.WikiPage{
		ID: "unverified", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/old",
		PageType: "concept", Status: "published", Content: "UNKNOWN_HISTORY_NEVER_REPUBLISH", Version: 9,
		SourceRefs: types.StringArray{"public-two-doc", "unrelated-unproved-document"},
	}
	require.NoError(t, db.Create(old).Error)
	original := types.TaskPendingOp{
		TenantID: 1, Scope: "knowledge_base", ScopeID: "public-two", TaskType: "wiki:ingest",
		Op: "ingest", DedupKey: "public-two-doc", Payload: json.RawMessage(`{"attempt":7}`), FailCount: 2,
	}
	require.NoError(t, db.Create(&original).Error)
	retract := types.TaskPendingOp{
		TenantID: 1, Scope: "knowledge_base", ScopeID: "public-two", TaskType: "wiki:ingest",
		Op: "retract", DedupKey: "public-two-doc",
		Payload: json.RawMessage(`{"op":"retract","knowledge_id":"public-two-doc",` +
			`"doc_summary":"OLD_QUEUE_BODY_NEVER_GENERATE"}`),
	}
	require.NoError(t, db.Create(&retract).Error)
	require.NoError(t, repository.WriteWikiProcessingBlock(ctx, repo, 1, "public-two", "verified_regeneration_required"))
	preview := func() (*repository.WikiPageRecoveryPlan, error) {
		return repository.PrepareWikiPageRecovery(ctx, db, 1, "public-two", "", false)
	}
	plan, err := preview()
	require.NoError(t, err)
	require.Equal(t, 1, plan.RetainedPages)
	require.Equal(t, []string{"unverified"}, plan.IsolatedPages)
	require.Empty(t, plan.QueueDocuments, "existing durable ingest is reused")
	require.Equal(t, []int64{retract.ID}, plan.RetiredRetracts)
	_, err = repository.PrepareWikiPageRecovery(ctx, db, 99, "public-two", plan.Snapshot, true)
	require.Error(t, err)
	_, err = repository.PrepareWikiPageRecovery(ctx, db, 1, "public-two", "stale", true)
	require.ErrorContains(t, err, "snapshot changed")
	claim := time.Now().UTC()
	require.NoError(t, db.Model(&original).Update("claimed_at", claim).Error)
	_, err = preview()
	require.ErrorContains(t, err, "not safely resumable")
	require.NoError(t, db.Model(&original).Update("claimed_at", nil).Error)
	plan, err = preview()
	require.NoError(t, err)
	applied, err := repository.PrepareWikiPageRecovery(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var current types.WikiPage
	require.NoError(t, db.First(&current, "id='valid'").Error)
	require.Equal(t, *valid, current, "valid page identity, body and version stay unchanged")
	current = types.WikiPage{}
	require.NoError(t, db.Unscoped().First(&current, "id='unverified'").Error)
	require.True(t, current.DeletedAt.Valid)
	require.Equal(t, old.Content, current.Content, "original body remains for audit only")
	var retained types.TaskPendingOp
	require.NoError(t, db.First(&retained, original.ID).Error)
	require.Equal(t, original.ID, retained.ID)
	require.Equal(t, original.FailCount, retained.FailCount)
	require.Equal(t, string(original.Payload), string(retained.Payload))
	require.Equal(t, "knowledge_base", retained.Scope)
	retained = types.TaskPendingOp{}
	require.NoError(t, db.First(&retained, retract.ID).Error)
	require.Equal(t, retract.ID, retained.ID)
	require.Equal(t, string(retract.Payload), string(retained.Payload))
	require.Equal(t, "wiki_regeneration_archive", retained.Scope)
	var seals int64
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id='unverified'").Count(&seals).Error)
	require.Zero(t, seals, "no retrospective proof is created")
	_, err = repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	_, err = repository.PrepareWikiPageRecovery(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.ErrorContains(t, err, "snapshot changed")
}

func TestWikiSelectiveRecoveryRejectsDeniedSourcesBeforeChangingPages(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id='public-two-doc'").
		Update("enable_status", "disabled").Error)
	_, err := repository.PrepareWikiPageRecovery(context.Background(), db, 1, "public-two", "", false)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&n).Error)
	require.Zero(t, n)
}
