package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMemberDeletionOfEnrolledFeishuDocumentPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tenant := types.Tenant{ID: 876501, Name: "local-delete", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	kb := types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "delete", Type: "document"}
	require.NoError(t, db.Create(&kb).Error)
	source := types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "feishu", Status: "active", LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}
	require.NoError(t, db.Create(&source).Error)
	meta, err := json.Marshal(map[string]string{"datasource_id": source.ID, "external_id": "member-delete"})
	require.NoError(t, err)
	k := types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Channel: "feishu", Type: "file", Title: "remove exactly this file", Metadata: meta,
		ParseStatus: types.ParseStatusCompleted,
	}
	sibling := types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "file", Title: "keep sibling", ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, db.Create(&k).Error)
	require.NoError(t, db.Create(&sibling).Error)
	chunk := types.Chunk{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, KnowledgeID: k.ID,
		Content: "synthetic local text", ChunkType: types.ChunkTypeText, IsEnabled: true,
	}
	require.NoError(t, db.Create(&chunk).Error)
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,
				content,dimension,embedding,is_enabled)
 VALUES(?,0,?,?,?,'synthetic local text',3,'[0.1,0.2,0.3]'::halfvec,true)`,
			chunk.ID,
			chunk.ID,
			k.ID,
			kb.ID,
		).Error,
	)
	r := types.FeishuLegacyResource{
		ID: legacyResourceID(&source, "member-delete"), TenantID: tenant.ID,
		KnowledgeBaseID: kb.ID, DataSourceID: source.ID, ExternalID: "member-delete", KnowledgeID: k.ID,
		Generation: 1, State: "active", QuarantineSnapshot: json.RawMessage(`{}`),
	}
	a := types.FeishuLegacyAttempt{
		ID: uuid.NewString(), ResourceID: r.ID, Generation: 1,
		Status: "published", LeaseUntil: time.Now().Add(-time.Hour), Manifest: json.RawMessage(`{}`),
		Receipt: json.RawMessage(`{}`),
	}
	r.CurrentAttempt, r.ActiveAttempt = a.ID, a.ID
	require.NoError(t, db.Create(&r).Error)
	require.NoError(t, db.Create(&a).Error)
	log := types.SyncLog{
		ID: uuid.NewString(), DataSourceID: source.ID, TenantID: tenant.ID, Status: "running",
		ExecutionClaimed: true, ExecutionGeneration: 1,
	}
	require.NoError(t, db.Create(&log).Error)
	db = types.EnableLegacyReadGuards(db)
	repo := NewKnowledgeRepository(db)
	rows, err := repo.GetKnowledgeDeletionMetadata(context.Background(), tenant.ID, []string{k.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, repo.MarkKnowledgeDeleting(context.Background(), rows[0]))
	require.NoError(t, db.First(&log, "id=?", log.ID).Error)
	require.Equal(t, types.SyncLogStatusCanceled, log.Status)
	require.False(t, log.ExecutionClaimed)
	require.EqualValues(t, 2, log.ExecutionGeneration)
	// The removal capability must never become a content-update capability.
	require.Error(t, db.Model(&types.Knowledge{}).Where("id=?", k.ID).Update("title", "stale writer").Error)
	require.Error(t, db.Model(&types.Chunk{}).Where("id=?", chunk.ID).Update("content", "stale writer").Error)
	require.Error(t, db.Exec("UPDATE embeddings SET content='stale writer' WHERE knowledge_id=?", k.ID).Error)
	require.NoError(t, db.Exec("DELETE FROM embeddings WHERE knowledge_id=?", k.ID).Error)
	require.NoError(
		t,
		NewChunkRepository(db).DeleteByKnowledgeList(context.Background(), tenant.ID, []string{k.ID}),
	)
	require.NoError(t, repo.DeleteKnowledgeList(context.Background(), tenant.ID, []string{k.ID}))
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", k.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", sibling.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.First(&r, "id=?", r.ID).Error)
	require.Equal(t, "deleted", r.State)
	require.Contains(t, string(r.QuarantineSnapshot), `"deleted_by_user": true`)
	require.Empty(t, r.CurrentAttempt)
	require.NoError(t, db.First(&source, "id=?", source.ID).Error)
	require.Equal(t, "hold", source.LegacyAccessState, "deletion must not revive remote authority")
}
