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
	applied, err := repository.PrepareWikiPreservingRecovery(
		ctx, db, 1, "public-two", plan.Snapshot, true)
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

func TestWikiPreservingRecoveryArchivesOnlyVerifiedReplacementRetractions(t *testing.T) {
	for _, scenario := range []string{
		"valid", "stale_snapshot", "missing_ingest", "wrong_identity", "claimed", "revoked_source",
		"duplicate_ingest", "deleted", "hard_deleted", "foreign_deleted_chunk",
		"revived_after_preview", "unproved_absence",
	} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			db := f.DB
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
			require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
				Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			page := types.WikiPage{
				ID: "retained-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/old",
				Title: "historical", Content: "original browser body",
				PageType: "concept", Status: "published", Version: 1,
				SourceRefs: types.StringArray{"public-two-doc"},
			}
			require.NoError(t, db.Create(&page).Error)
			makeOp := func(kind string) *types.TaskPendingOp {
				body, err := json.Marshal(map[string]any{"op": kind, "knowledge_id": "public-two-doc", "attempt": 1})
				require.NoError(t, err)
				op := &types.TaskPendingOp{
					TenantID:   1,
					Scope:      "knowledge_base",
					ScopeID:    "public-two",
					TaskType:   "wiki:ingest",
					Op:         kind,
					DedupKey:   "public-two-doc",
					Payload:    body,
					FailCount:  2,
					EnqueuedAt: time.Now().UTC().Truncate(time.Second),
				}
				require.NoError(t, db.Create(op).Error)
				return op
			}
			retract := makeOp("retract")
			deletedScenario := scenario == "deleted" || scenario == "revived_after_preview" ||
				scenario == "unproved_absence" || scenario == "hard_deleted" || scenario == "foreign_deleted_chunk"
			if deletedScenario {
				body, err := json.Marshal(map[string]any{"op": "retract", "knowledge_id": "deleted-source"})
				require.NoError(t, err)
				retract.DedupKey = "deleted-source"
				retract.Payload = body
				require.NoError(t, db.Save(retract).Error)
				if scenario != "unproved_absence" {
					require.NoError(t, db.Create(&types.Knowledge{
						ID: "deleted-source", TenantID: 1, KnowledgeBaseID: "public-two", Title: "deleted source", Type: "file",
						ParseStatus: "completed", EnableStatus: "enabled", Channel: "web",
					}).Error)
					require.NoError(
						t,
						db.Exec("UPDATE knowledges SET deleted_at=CURRENT_TIMESTAMP WHERE id='deleted-source'").Error,
					)
				}
			}
			if scenario == "hard_deleted" || scenario == "foreign_deleted_chunk" {
				require.NoError(t, db.Create(&types.Chunk{
					ID: "old-deleted-chunk", TenantID: 1, KnowledgeBaseID: "public-two", KnowledgeID: "deleted-source",
					Content: "old deleted input", ChunkType: types.ChunkTypeText, IsEnabled: true,
				}).Error)
				require.NoError(
					t,
					db.Exec("UPDATE chunks SET deleted_at=CURRENT_TIMESTAMP WHERE id='old-deleted-chunk'").Error,
				)
				require.NoError(t, db.Exec("DELETE FROM knowledges WHERE id='deleted-source'").Error)
				if scenario == "foreign_deleted_chunk" {
					require.NoError(t, db.Exec("UPDATE chunks SET tenant_id=2 WHERE id='old-deleted-chunk'").Error)
				}
			}
			if scenario != "missing_ingest" {
				makeOp("ingest")
			}
			switch scenario {
			case "wrong_identity":
				require.NoError(
					t,
					db.Model(retract).
						Update("payload", json.RawMessage(`{"op":"retract","knowledge_id":"other"}`)).
						Error,
				)
			case "claimed":
				require.NoError(t, db.Model(retract).Update("claimed_at", time.Now()).Error)
			case "revoked_source":
				require.NoError(
					t,
					db.Model(&types.Knowledge{}).Where("id='public-two-doc'").Update("enable_status", "disabled").Error,
				)
			case "duplicate_ingest":
				makeOp("ingest")
			}
			ctx := context.Background()
			plan, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
			if scenario != "valid" && scenario != "stale_snapshot" && scenario != "deleted" &&
				scenario != "revived_after_preview" && scenario != "hard_deleted" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, []int64{retract.ID}, plan.RetiredRetracts)
				var before types.TaskPendingOp
				require.NoError(t, db.First(&before, retract.ID).Error)
				if scenario == "stale_snapshot" || scenario == "revived_after_preview" {
					if scenario == "revived_after_preview" {
						require.NoError(t,
							db.Exec("UPDATE knowledges SET deleted_at=NULL WHERE id='deleted-source'").Error)
					}
					require.NoError(t, db.Model(retract).Update("fail_count", 3).Error)
					_, err = repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", plan.Snapshot, true)
					require.Error(t, err)
				} else {
					applied, err := repository.PrepareWikiPreservingRecovery(
						ctx, db, 1, "public-two", plan.Snapshot, true)
					require.NoError(t, err)
					require.True(t, applied.Applied)
					var archived types.TaskPendingOp
					require.NoError(t, db.First(&archived, retract.ID).Error)
					require.Equal(t, "wiki_preserve_retract_archive", archived.Scope)
					archived.Scope = before.Scope
					require.Equal(t, before, archived)
					var active int64
					require.NoError(t, db.Model(&types.TaskPendingOp{}).
						Where("scope='knowledge_base' AND scope_id='public-two'").Count(&active).Error)
					require.EqualValues(t, 1, active, "retain the verified replacement ingest")
				}
			}
			var stored types.WikiPage
			require.NoError(t, db.First(&stored, "id=?", page.ID).Error)
			require.Equal(t, page.Content, stored.Content)
			require.Equal(t, "published", stored.Status)
			if scenario != "valid" && scenario != "deleted" && scenario != "hard_deleted" {
				var pending types.TaskPendingOp
				require.NoError(t, db.First(&pending, retract.ID).Error)
				require.Equal(t, "knowledge_base", pending.Scope)
				var markers int64
				require.NoError(
					t,
					db.Model(&types.TaskPendingOp{}).Where("scope='wiki_preserving_recovery'").Count(&markers).Error,
				)
				require.Zero(t, markers)
			}
		})
	}
}
