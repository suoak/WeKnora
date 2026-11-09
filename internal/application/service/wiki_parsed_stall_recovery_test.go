package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiParsedStallRecoveryPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	f := wikiPendingRegenerationFixture(t)
	f.PreparePublished(t)
	db, ctx := f.DB, context.Background()
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,status,
 legacy_access_state,config) VALUES('gitlab',1,'public-two','synthetic','gitlab','active','unmanaged','{}')`).Error)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	originalError := "task stuck in finalizing: no progress since 2026-11-04T20:09:44Z (> 2h10m0s), recovered by housekeeping"
	require.NoError(
		t,
		db.Exec(`UPDATE knowledges SET channel='gitlab',parse_status='failed',summary_status='completed',
 pending_subtasks_count=0,error_message=?,
 metadata='{"datasource_id":"gitlab","external_id":"gitlab:synthetic:main:doc.md"}' WHERE id='public-two-doc'`,
			originalError).Error,
	)
	require.NoError(t, db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error)
	require.NoError(
		t,
		db.Exec(`CREATE TABLE IF NOT EXISTS embeddings(id bigserial PRIMARY KEY,knowledge_id text,
 knowledge_base_id text,chunk_id text,source_id text,source_type int,content text,dimension int,
 embedding halfvec,is_enabled boolean)`).Error,
	)
	require.NoError(
		t,
		db.Exec(`INSERT INTO embeddings(knowledge_id,knowledge_base_id,chunk_id,source_id,source_type,
 content,dimension,embedding,is_enabled) SELECT knowledge_id,knowledge_base_id,id,id,0,content,1,'[1]'::halfvec,true
 FROM chunks WHERE knowledge_id='public-two-doc' AND chunk_type='text'`).Error,
	)
	for _, name := range []string{"docreader", "chunking", "embedding"} {
		require.NoError(t, db.Create(&types.KnowledgeProcessingSpan{
			KnowledgeID: "public-two-doc",
			Attempt:     7, SpanID: "parsed-" + name, Name: name, Kind: types.SpanKindStage, Status: types.SpanStatusDone,
		}).Error)
	}
	op := types.TaskPendingOp{
		TenantID: 1, TaskType: types.TypeWikiIngest, Scope: types.TaskScopeKnowledgeBase,
		ScopeID: "public-two", Op: "ingest", DedupKey: "public-two-doc", FailCount: 2, Payload: json.RawMessage(`{"attempt":7}`),
	}
	require.NoError(t, db.Create(&op).Error)
	preview, err := repository.PrepareWikiParsedStallRecovery(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.Len(t, preview.Documents, 1)
	require.Equal(t, originalError, preview.Documents[0].OriginalError)
	var row types.Knowledge
	require.NoError(t, db.Where("id='public-two-doc'").First(&row).Error)
	require.Equal(t, "failed", row.ParseStatus, "preview is read only")
	rollback := errors.New("rollback test mutation")
	for _, mutation := range []string{
		"DELETE FROM embeddings WHERE knowledge_id='public-two-doc'",
		"UPDATE data_sources SET legacy_access_state='hold' WHERE id='gitlab'",
		"UPDATE knowledge_processing_spans SET status='running' WHERE name='embedding'",
		"UPDATE knowledges SET error_message='real parser failure' WHERE id='public-two-doc'",
		"UPDATE task_pending_ops SET claimed_at=now() WHERE scope='knowledge_base'",
		"UPDATE chunks SET tenant_id=99 WHERE knowledge_id='public-two-doc'",
	} {
		require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec(mutation).Error)
			_, failure := repository.PrepareWikiParsedStallRecovery(ctx, tx, 1, "public-two", "", false)
			require.Error(t, failure, mutation)
			return rollback
		}), rollback)
	}
	_, err = repository.PrepareWikiParsedStallRecovery(ctx, db, 1, "public-two", "stale", true)
	require.Error(t, err)
	_, err = repository.PrepareWikiParsedStallRecovery(ctx, db, 99, "public-two", preview.Snapshot, true)
	require.Error(t, err)
	applied, err := repository.PrepareWikiParsedStallRecovery(ctx, db, 1, "public-two", preview.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	row = types.Knowledge{}
	require.NoError(t, db.Where("id='public-two-doc'").First(&row).Error)
	require.Equal(t, types.ParseStatusFinalizing, row.ParseStatus, "normal worker owns final completion")
	require.Equal(t, 1, row.PendingSubtasksCount)
	require.Equal(t, "completed", row.SummaryStatus)
	var retained types.TaskPendingOp
	require.NoError(t, db.First(&retained, op.ID).Error)
	require.Equal(t, op.FailCount, retained.FailCount)
	require.Equal(t, string(op.Payload), string(retained.Payload))
	require.Nil(t, retained.ClaimedAt)
	_, err = repository.BeginWikiMCPGeneration(ctx, repository.NewWikiPageRepository(db), "public-two")
	require.NoError(t, err, "normal generation must succeed without the exceptional recovery preview set")
	var audit int64
	require.NoError(
		t,
		db.Table("task_pending_ops").Where("scope='wiki_parsed_recovery_audit'").Count(&audit).Error,
	)
	require.EqualValues(t, 1, audit)
	_, err = repository.PrepareWikiParsedStallRecovery(ctx, db, 1, "public-two", preview.Snapshot, true)
	require.Error(t, err, "recovery cannot be replayed")
}
