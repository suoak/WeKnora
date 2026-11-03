package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func seedEmptyWikiRetraction(t *testing.T, f *fixture.Fixture) types.TaskPendingOp {
	t.Helper()
	f.PreparePublished(t)
	require.NoError(t, f.DB.AutoMigrate(&types.TaskPendingOp{}, &types.TaskDeadLetter{}))
	require.NoError(t, f.DB.Exec(`UPDATE knowledges SET deleted_at=? WHERE id='public-two-doc'`, time.Now()).Error)
	op := types.TaskPendingOp{
		TenantID: 1, TaskType: types.TypeWikiIngest, Scope: types.TaskScopeKnowledgeBase,
		ScopeID: "public-two", Op: "retract", DedupKey: "public-two-doc",
		Payload: []byte(`{"op":"retract","knowledge_id":"public-two-doc","doc_summary":"OLD_PRIVATE_QUEUE_CANARY"}`),
	}
	require.NoError(t, f.DB.Create(&op).Error)
	require.NoError(t, f.DB.Create(&types.WikiPage{
		ID: "unrelated-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "index",
		Content: "UNVERIFIED_OLD_WIKI_CANARY", Status: "published", Version: 7,
		SourceRefs: types.StringArray{"another-document|unrelated"},
	}).Error)
	var stored types.TaskPendingOp
	require.NoError(t, f.DB.First(&stored, op.ID).Error)
	return stored
}

func TestWikiEmptyRetractionPreservesRowsPagesAndSourceRestriction(t *testing.T) {
	testWikiEmptyRetractionRetirement(t, fixture.New(t))
}

func testWikiEmptyRetractionRetirement(t *testing.T, f *fixture.Fixture) {
	op := seedEmptyWikiRetraction(t, f)
	db := f.DB
	require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('held-source',1,'public-two','synthetic','feishu','error','hold','{}')`).Error)
	db = types.EnableLegacyReadGuards(db)
	ctx := context.Background()
	preview, err := repository.PrepareWikiEmptyRetractions(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, preview.PendingOperations)
	require.Equal(t, 1, preview.CurrentPages, "hidden pages must still participate in absence verification")
	var auditCount int64
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&auditCount).Error)
	require.Zero(t, auditCount, "preview must remain read-only")
	applied, err := repository.PrepareWikiEmptyRetractions(ctx, db, 1, "public-two", preview.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var retained types.TaskPendingOp
	require.NoError(t, db.First(&retained, op.ID).Error)
	require.Equal(t, "wiki_regeneration_archive", retained.Scope)
	retained.Scope = op.Scope
	require.True(t, retained.EnqueuedAt.Equal(op.EnqueuedAt))
	retained.EnqueuedAt = retained.EnqueuedAt.UTC()
	op.EnqueuedAt = op.EnqueuedAt.UTC()
	require.Equal(t, op, retained, "only the operation's consumable scope may change")
	var page struct {
		Content string
		Version int
	}
	require.NoError(t, db.Raw("SELECT content,version FROM wiki_pages WHERE id='unrelated-page'").Scan(&page).Error)
	require.Equal(t, "UNVERIFIED_OLD_WIKI_CANARY", page.Content)
	require.Equal(t, 7, page.Version)
	_, err = repository.BeginWikiMCPGeneration(ctx, repository.NewWikiPageRepository(db), "public-two")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "retiring an empty cleanup confers no model authority")
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&auditCount).Error)
	require.EqualValues(t, 1, auditCount)
	_, err = repository.PrepareWikiEmptyRetractions(ctx, db, 1, "public-two", preview.Snapshot, true)
	require.Error(t, err, "an already retired operation cannot be applied again")
}

func TestWikiEmptyRetractionDoesNotMistakeHiddenReferencesForAbsence(t *testing.T) {
	testWikiEmptyRetractionHiddenReference(t, fixture.New(t))
}

func testWikiEmptyRetractionHiddenReference(t *testing.T, f *fixture.Fixture) {
	op := seedEmptyWikiRetraction(t, f)
	db := f.DB
	require.NoError(t, db.Exec(`UPDATE wiki_pages SET source_refs='["public-two-doc|hidden"]'
 WHERE id='unrelated-page'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('held-source',1,'public-two','synthetic','feishu','error','hold','{}')`).Error)
	db = types.EnableLegacyReadGuards(db)
	var visible int64
	require.NoError(t, db.Table("wiki_pages").Where("knowledge_base_id='public-two'").Count(&visible).Error)
	require.Zero(t, visible, "the ordinary read guard intentionally hides these pages")
	_, err := repository.PrepareWikiEmptyRetractions(context.Background(), db, 1, "public-two", "", false)
	require.ErrorContains(t, err, "reference remains")
	var retained types.TaskPendingOp
	require.NoError(t, db.First(&retained, op.ID).Error)
	require.Equal(t, op.Scope, retained.Scope)
}

func TestWikiEmptyRetractionRefusesChangedClaimLiveDocumentAndMalformedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"live_document", `UPDATE knowledges SET deleted_at=NULL WHERE id='public-two-doc'`},
		{"live_claim", `UPDATE task_pending_ops SET claimed_at=CURRENT_TIMESTAMP
 WHERE scope='knowledge_base'`},
		{"new_reference", `UPDATE wiki_pages SET source_refs='["public-two-doc"]' WHERE id='unrelated-page'`},
		{"malformed_metadata", `UPDATE wiki_pages SET source_refs='"unknown"' WHERE id='unrelated-page'`},
		{"epoch_change", `UPDATE wiki_pages SET content='intervening body' WHERE id='unrelated-page'`},
		{"queued_page_target", `UPDATE task_pending_ops SET payload=CAST(
 '{"op":"retract","knowledge_id":"public-two-doc","page_slugs":["index"]}' AS BLOB)
 WHERE scope='knowledge_base'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.New(t)
			op := seedEmptyWikiRetraction(t, f)
			preview, err := repository.PrepareWikiEmptyRetractions(
				context.Background(), f.DB, 1, "public-two", "", false,
			)
			require.NoError(t, err)
			require.NoError(t, f.DB.Exec(tc.sql).Error)
			_, err = repository.PrepareWikiEmptyRetractions(
				context.Background(), f.DB, 1, "public-two", preview.Snapshot, true,
			)
			require.Error(t, err)
			var retained types.TaskPendingOp
			require.NoError(t, f.DB.First(&retained, op.ID).Error)
			require.Equal(t, op.Scope, retained.Scope)
		})
	}
}
