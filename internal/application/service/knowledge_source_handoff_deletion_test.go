package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalDocumentDeletionDedicatedSourceHandoff(t *testing.T) {
	for _, scenario := range []string{"idle_hold", "running", "shared_link", "enrolled", "stale_metadata", "foreign_source"} {
		t.Run(scenario, func(t *testing.T) {
			f, proof, _ := exactExecutionFixture(t)
			require.NoError(t, f.db.AutoMigrate(&types.SyncLog{}))
			require.NoError(t, f.db.Exec("ALTER TABLE feishu_legacy_resources ADD COLUMN data_source_id text").Error)
			var imported types.FeishuPersonalImport
			require.NoError(t, f.db.Where("id=?", proof.importID).First(&imported).Error)
			require.NoError(t, f.db.Exec("UPDATE feishu_personal_imports SET status='source_managed' WHERE id=?", imported.ID).Error)
			require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET status='obsolete',owner_nonce='',lease_until=NULL WHERE id=?", proof.attempt).Error)
			config, err := (&types.DataSourceConfig{ResourceIDs: []string{"links"}, Settings: map[string]interface{}{"links": []string{imported.URL}}}).ToJSON()
			require.NoError(t, err)
			if scenario == "shared_link" {
				config = types.JSON(`{"resource_ids":["links"],"settings":{"links":["other","another"]}}`)
			}
			source := &types.DataSource{ID: imported.ID, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu", Status: "error", LegacyAccessState: "hold", Config: config, LastSyncCursor: types.JSON(`{"keep":"cursor"}`)}
			if scenario == "foreign_source" {
				source.TenantID = 2
			}
			require.NoError(t, f.db.Create(source).Error)
			metadata, err := json.Marshal(map[string]string{"datasource_id": imported.ID})
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE knowledges SET metadata=? WHERE id=?", types.JSON(metadata), proof.knowledge).Error)
			if scenario == "running" {
				require.NoError(t, f.db.Create(&types.SyncLog{ID: "live", DataSourceID: source.ID, TenantID: 1, Status: "running", ExecutionClaimed: true}).Error)
			}
			if scenario == "enrolled" {
				require.NoError(t, f.db.Exec("INSERT INTO feishu_legacy_resources(id,data_source_id,knowledge_id) VALUES('enrolled',?,?)", source.ID, proof.knowledge).Error)
			}
			ctx := types.WithKnowledgeDeletionMetadata(personalDeletionWebContext("A"), proof.knowledge)
			row, err := f.ks.GetKnowledgeDeletionMetadata(ctx, proof.knowledge)
			require.NoError(t, err)
			if scenario == "stale_metadata" {
				require.NoError(t, f.db.Exec("UPDATE knowledges SET updated_at=updated_at+interval '1 second' WHERE id=?", row.ID).Error)
			}
			err = f.ks.repo.MarkKnowledgeDeleting(ctx, row)
			var stored types.DataSource
			require.NoError(t, f.db.Unscoped().Where("id=?", source.ID).First(&stored).Error)
			require.JSONEq(t, string(source.LastSyncCursor), string(stored.LastSyncCursor))
			if scenario != "idle_hold" {
				require.Error(t, err)
				require.False(t, stored.DeletedAt.Valid, "a rejected deletion must roll back source retirement")
				require.Equal(t, "hold", stored.LegacyAccessState)
				return
			}
			require.NoError(t, err)
			require.True(t, stored.DeletedAt.Valid)
			require.Equal(t, "revoked", stored.LegacyAccessState)
			require.NoError(t, f.ks.repo.MarkKnowledgeDeleting(ctx, row), "cleanup retry preserves the fence")
			var mapping types.FeishuPersonalImport
			require.NoError(t, f.db.Where("id=?", imported.ID).First(&mapping).Error)
			require.Equal(t, "deleted", mapping.Status)
			// Continue through the actual worker, including a duplicate delivery.
			require.NoError(t, f.db.AutoMigrate(&types.KnowledgeTagRelation{}, &types.KnowledgeTag{}, &types.ChunkRevision{}))
			f.ks.personalRecovery = f.p
			f.ks.chunkRepo = repository.NewChunkRepository(f.db)
			f.ks.graphEngine = &documentGraphSpy{}
			f.ks.tenantRepo = &documentTenantSpy{}
			payload := types.KnowledgeListDeletePayload{TenantID: 1, KnowledgeBaseID: proof.kb,
				KnowledgeIDs: []string{proof.knowledge}, Initiator: types.TaskInitiator{UserID: "A"}}
			for range 2 {
				require.NoError(t, f.ks.ProcessKnowledgeListDelete(context.Background(), deleteDocumentTask(t, payload)))
			}
			var count int64
			require.NoError(t, f.db.Table("knowledges").Where("id=? AND deleted_at IS NULL", proof.knowledge).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}
