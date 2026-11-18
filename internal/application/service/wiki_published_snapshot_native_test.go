//go:build anydoc && personal_exact_live

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

func TestWikiPublishedSnapshotSeparatesBrowserFromDraftGeneration(t *testing.T) {
	t.Setenv("WIKI_SELECTION_NATIVE_TEST", "true")
	f := prepareWikiSelectionFixture(t)
	db := types.EnableLegacyReadGuards(f.DB)
	require.NoError(t, db.AutoMigrate(&types.WikiPageRevision{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	repo := repository.NewWikiPageRepository(db)
	old := &types.WikiPage{
		ID: "published-before-rebuild", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/current",
		Title: "Original published title", Content: "ORIGINAL_PUBLISHED_BROWSER_ONLY", Summary: "old summary",
		PageType: "summary", Status: "published", Version: 4, SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, db.Create(old).Error)
	var original json.RawMessage
	require.NoError(t, db.Raw("SELECT to_jsonb(p) FROM wiki_pages p WHERE id=?", old.ID).
		Row().Scan(&original))
	recovery, err := repository.PrepareWikiPreservingRecovery(
		context.Background(), db, 1, old.KnowledgeBaseID, "", false)
	require.NoError(t, err)
	_, err = repository.PrepareWikiPreservingRecovery(
		context.Background(), db, 1, old.KnowledgeBaseID, recovery.Snapshot, true)
	require.NoError(t, err)
	work, err := repository.BeginWikiPreservingGeneration(context.Background(), repo, old.KnowledgeBaseID)
	require.NoError(t, err)
	work = types.WithWikiEditSource(work, types.WikiEditSourcePipeline)
	candidate := &types.WikiPage{
		ID: "temporary-candidate", TenantID: old.TenantID, KnowledgeBaseID: old.KnowledgeBaseID, Slug: old.Slug,
		Title: "New unfinished title", Content: "CURRENT_VERIFIED_DRAFT", PageType: "summary", Status: "draft",
		SourceRefs: old.SourceRefs, Version: 1,
	}
	_, err = (&wikiPageService{repo: repo}).CreatePage(work, candidate)
	require.NoError(t, err)
	require.Equal(t, old.ID, candidate.ID)
	generated, err := repo.GetBySlug(work, old.KnowledgeBaseID, old.Slug)
	require.NoError(t, err)
	require.Equal(t, "draft", generated.Status)
	require.Equal(t, candidate.Content, generated.Content)
	require.NotContains(t, generated.Content, old.Content, "historical content must never enter model reads")

	web := fixture.Web("A", 1)
	require.NoError(t, db.Create(&types.WikiFolder{
		ID: "snapshot-folder", TenantID: 1, KnowledgeBaseID: old.KnowledgeBaseID,
		Name: "Folder", Path: "Folder",
	}).Error)
	paths, err := repo.ListDistinctCategoryPaths(web, old.KnowledgeBaseID, 20)
	require.NoError(t, err, "directory projections must keep querying wiki_folders")
	require.Contains(t, paths, []string{"Folder"})
	// A fresh repository/context simulates a worker or browser restart.
	restarted := repository.NewWikiPageRepository(db)
	served, err := restarted.GetByID(web, old.ID)
	require.NoError(t, err)
	require.Equal(t, "published", served.Status)
	require.Equal(t, old.Content, served.Content)
	require.Equal(t, old.Version, served.Version)
	listed, count, err := restarted.List(web, &types.WikiPageListRequest{
		KnowledgeBaseID: old.KnowledgeBaseID, Status: "published", Query: "ORIGINAL_PUBLISHED_BROWSER_ONLY",
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, listed, 1)
	require.Equal(t, old.Content, listed[0].Content, "filtering and counts must use the visible snapshot")
	_, count, err = restarted.List(web, &types.WikiPageListRequest{
		KnowledgeBaseID: old.KnowledgeBaseID, Status: "published", Query: "CURRENT_VERIFIED_DRAFT",
	})
	require.NoError(t, err)
	require.Zero(t, count, "draft content must not enter published search")

	_, err = restarted.GetByID(fixture.Web("A", 2), old.ID)
	require.Error(t, err, "another tenant cannot obtain a snapshot")
	background, err := restarted.GetByID(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, candidate.Content, background.Content)
	strict := types.WithWikiVerifiedPageReadGuard(web, old.KnowledgeBaseID, []string{old.ID},
		func(_ *gorm.DB, _, _ string, page *types.WikiPage) error {
			if page != nil {
				require.Equal(t, candidate.Content, page.Content)
			}
			return nil
		})
	strictPage, err := restarted.GetByID(strict, old.ID)
	require.NoError(t, err)
	require.Equal(t, candidate.Content, strictPage.Content,
		"current-provenance views cannot substitute uncertified historical text")
	t.Run("recover-existing-hidden-page-from-bound-backup", func(t *testing.T) {
		tx := db.Begin()
		defer tx.Rollback()
		require.NoError(t, tx.Where("scope='wiki_published_snapshot'").Delete(&types.TaskPendingOp{}).Error)
		plan, err := repository.RestoreWikiPublishedSnapshots(
			context.Background(), tx, 1, old.KnowledgeBaseID, []json.RawMessage{original}, "", false)
		require.NoError(t, err)
		require.Equal(t, []string{old.ID}, plan.Restore)
		require.False(t, plan.Applied)
		_, err = repository.RestoreWikiPublishedSnapshots(
			context.Background(),
			tx,
			1,
			old.KnowledgeBaseID,
			[]json.RawMessage{original},
			plan.Snapshot,
			true,
		)
		require.NoError(t, err)
		served, err := repository.NewWikiPageRepository(tx).GetByID(web, old.ID)
		require.NoError(t, err)
		require.Equal(t, old.Content, served.Content)
		actual, err := repository.NewWikiPageRepository(tx).GetByID(context.Background(), old.ID)
		require.NoError(t, err)
		require.Equal(t, "draft", actual.Status, "repair must not publish unfinished output")
		require.Equal(t, candidate.Content, actual.Content)
	})
	t.Run("backup-tamper-and-concurrent-edit-refused", func(t *testing.T) {
		tx := db.Begin()
		defer tx.Rollback()
		require.NoError(t, tx.Where("scope='wiki_published_snapshot'").Delete(&types.TaskPendingOp{}).Error)
		var changed map[string]any
		require.NoError(t, json.Unmarshal(original, &changed))
		changed["content"] = "UNPROVEN_BACKUP_CONTENT"
		bad, err := json.Marshal(changed)
		require.NoError(t, err)
		_, err = repository.RestoreWikiPublishedSnapshots(
			context.Background(), tx, 1, old.KnowledgeBaseID, []json.RawMessage{bad}, "", false)
		require.Error(t, err)
		plan, err := repository.RestoreWikiPublishedSnapshots(
			context.Background(), tx, 1, old.KnowledgeBaseID, []json.RawMessage{original}, "", false)
		require.NoError(t, err)
		require.NoError(t, tx.Exec("UPDATE wiki_pages SET title='concurrent edit' WHERE id=?", old.ID).Error)
		_, err = repository.RestoreWikiPublishedSnapshots(
			context.Background(),
			tx,
			1,
			old.KnowledgeBaseID,
			[]json.RawMessage{original},
			plan.Snapshot,
			true,
		)
		require.Error(t, err)
		var count int64
		require.NoError(
			t,
			tx.Model(&types.TaskPendingOp{}).Where("scope='wiki_published_snapshot'").Count(&count).Error,
		)
		require.Zero(t, count)
	})

	t.Run("revoked-source", func(t *testing.T) {
		tx := db.Begin()
		defer tx.Rollback()
		require.NoError(t, tx.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,
 status,legacy_access_state,config) VALUES('snapshot-revoked',1,'public-two','test','feishu_drive',
 'active','revoked','{}')`).Error)
		_, err := repository.NewWikiPageRepository(tx).GetByID(web, old.ID)
		require.Error(t, err, "retained content must obey current source read restrictions")
	})
	t.Run("publish-rollback", func(t *testing.T) {
		tx := db.Begin()
		defer tx.Rollback()
		require.NoError(t, tx.Exec(`CREATE FUNCTION snapshot_publish_fault() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN IF OLD.scope='wiki_published_snapshot'
 THEN RAISE EXCEPTION 'synthetic snapshot retirement failure'; END IF; RETURN OLD; END $$;
 CREATE TRIGGER snapshot_publish_fault BEFORE DELETE ON task_pending_ops
 FOR EACH ROW EXECUTE FUNCTION snapshot_publish_fault()`).Error)
		local := repository.NewWikiPageRepository(tx)
		page, err := local.GetBySlug(work, old.KnowledgeBaseID, old.Slug)
		require.NoError(t, err)
		page.Status = "published"
		require.Error(t, local.UpdateMeta(work, page))
		served, err := local.GetByID(web, old.ID)
		require.NoError(t, err)
		require.Equal(t, old.Content, served.Content)
	})
	generated.Status = "published"
	require.NoError(t, repo.UpdateMeta(work, generated))
	served, err = restarted.GetByID(web, old.ID)
	require.NoError(t, err)
	require.Equal(t, candidate.Content, served.Content)
	require.Equal(t, "published", served.Status)
	var snapshots int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).
		Where("scope='wiki_published_snapshot' AND dedup_key=?", old.ID).Count(&snapshots).Error)
	require.Zero(t, snapshots, "publication and snapshot retirement commit together")
}
