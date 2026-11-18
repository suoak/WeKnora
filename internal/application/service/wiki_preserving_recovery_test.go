package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiPreservingRecoverySnapshotQueuesWithoutHidingPages(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(
		t,
		db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}, &types.WikiPageRevision{}),
	)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	old := types.WikiPage{
		ID: "historical", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "old/concept", Title: "Legacy",
		Content: "HISTORICAL_CONTENT_NOT_FOR_MODEL", PageType: "concept", Status: "published", Version: 2,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, db.Create(&old).Error)
	ctx := context.Background()
	plan, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, plan.UnverifiedPages)
	require.Equal(t, []string{"public-two-doc"}, plan.Documents)
	_, err = repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", "stale", true)
	require.ErrorContains(t, err, "snapshot changed")
	var count int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&count).Error)
	require.Zero(t, count)
	applied, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var current types.WikiPage
	require.NoError(t, db.First(&current, "id=?", old.ID).Error)
	require.Equal(t, old.Content, current.Content)
	require.False(t, current.DeletedAt.Valid)
	repo := repository.NewWikiPageRepository(db)
	active, err := repository.WikiPreservingRecoveryActive(ctx, repo, "public-two")
	require.NoError(t, err)
	require.True(t, active)
	_, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
	require.ErrorContains(t, err, "still pending")
	// Only the synthetic fixture removes its own task to model successful consumption.
	require.NoError(
		t,
		db.Where("scope='knowledge_base' AND scope_id='public-two'").Delete(&types.TaskPendingOp{}).Error,
	)
	_, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
	require.ErrorContains(t, err, "coverage is incomplete")
	worker, err := repository.BeginWikiPreservingGeneration(ctx, repo, "public-two")
	require.NoError(t, err)
	fresh := &types.WikiPage{
		ID: "current-summary", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/current",
		Title: "Current", Content: "CURRENT_VERIFIED_SUMMARY", PageType: "summary", Status: "published", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(worker, fresh))
	finish, err := repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.Equal(t, []string{old.ID}, finish.RetiredPages)
	require.Equal(t, 1, finish.RetainedPages)
	_, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "stale", true)
	require.ErrorContains(t, err, "snapshot changed")
	current = types.WikiPage{}
	require.NoError(t, db.First(&current, "id=?", old.ID).Error)
	require.False(t, current.DeletedAt.Valid, "a rejected finish cannot hide the historical page")
	_, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", finish.Snapshot, true)
	require.NoError(t, err)
	active, err = repository.WikiPreservingRecoveryActive(ctx, repo, "public-two")
	require.NoError(t, err)
	require.False(t, active)
	current = types.WikiPage{}
	require.NoError(t, db.Unscoped().First(&current, "id=?", old.ID).Error)
	require.True(t, current.DeletedAt.Valid)
	require.Equal(t, old.Content, current.Content, "obsolete body remains in audit storage")
	_, err = repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err, "ordinary strict generation works after completion")
}

func TestWikiPreservingRecoveryRejectsDeniedSourceBeforeChanges(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	require.NoError(
		t,
		db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("enable_status", "disabled").Error,
	)
	_, err := repository.PrepareWikiPreservingRecovery(context.Background(), db, 1, "public-two", "", false)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&count).Error)
	require.Zero(t, count)
}
