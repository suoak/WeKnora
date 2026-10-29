package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDeleteUnusedFeishuSourcePreservesKnowledgeBaseAccess(t *testing.T) {
	for _, tc := range []struct {
		name, history string
		wantRevoked   bool
	}{
		{"never-used", "", false},
		{"unrelated-document", `INSERT INTO knowledges VALUES('doc','kb','{"datasource_id":"other"}',NULL)`, false},
		{"pending-sync", `INSERT INTO sync_logs(id,data_source_id,status) VALUES('run','source','running')`, true},
		{"stored-document", `INSERT INTO knowledges VALUES('doc','kb','{"datasource_id":"source"}',NULL)`, true},
		{"deleted-document", `INSERT INTO knowledges VALUES('doc','kb','{"datasource_id":"source"}',CURRENT_TIMESTAMP)`, true},
		{"attempted-publication", `INSERT INTO feishu_legacy_resources VALUES('kb','source','hold','candidate')`, true},
		{"historical-wiki", `INSERT INTO wiki_pages VALUES('kb',CURRENT_TIMESTAMP)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupKBTestDB(t)
			require.NoError(t, db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
			require.NoError(t, db.Exec(`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,metadata TEXT,deleted_at DATETIME);
CREATE TABLE feishu_legacy_resources(knowledge_base_id TEXT,data_source_id TEXT,state TEXT,knowledge_id TEXT);
CREATE TABLE wiki_pages(knowledge_base_id TEXT,deleted_at DATETIME);
INSERT INTO tenants VALUES(1,'active',NULL);`).Error)
			kb := makeKB(nil)
			kb.ID = "kb"
			require.NoError(t, db.Create(kb).Error)
			source := &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: kb.ID,
				Type: types.ConnectorTypeFeishu, LegacyAccessState: "hold", Status: "paused"}
			require.NoError(t, db.Create(source).Error)
			if tc.history != "" {
				require.NoError(t, db.Exec(tc.history).Error)
			}
			db = types.EnableLegacyReadGuards(db)
			require.NoError(t, NewDataSourceRepository(db).Delete(context.Background(), source.ID))
			var stored types.DataSource
			require.NoError(t, db.Unscoped().First(&stored, "id=?", source.ID).Error)
			require.True(t, stored.DeletedAt.Valid, "deletion must remain effective")
			require.Equal(t, "paused", stored.Status, "deletion must not enable sync")
			_, err := NewKnowledgeBaseRepository(db).GetKnowledgeBaseByID(context.Background(), kb.ID)
			if tc.wantRevoked {
				require.Equal(t, "revoked", stored.LegacyAccessState)
				require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
			} else {
				require.Equal(t, "unmanaged", stored.LegacyAccessState)
				require.NoError(t, err, "deleting an unused source cannot seal its KB")
			}
			_, claimed, err := NewSyncLogRepository(db).(interface {
				ClaimExecution(context.Context, string, string, uint64) (types.SyncExecution, bool, error)
			}).ClaimExecution(context.Background(), source.ID, "run", 1)
			require.NoError(t, err)
			require.False(t, claimed, "deleted sources cannot claim a later execution")
		})
	}
}

func TestDeleteUnusedFeishuSourcePostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.Exec(`CREATE TABLE knowledges(metadata JSONB);
CREATE TABLE feishu_legacy_resources(data_source_id TEXT);
CREATE TABLE wiki_pages(knowledge_base_id TEXT);`).Error)
	db = types.EnableLegacyReadGuards(db)
	for _, used := range []bool{false, true} {
		id := "empty"
		if used {
			id = "pending"
		}
		source := &types.DataSource{ID: id, TenantID: 1, KnowledgeBaseID: "kb",
			Type: types.ConnectorTypeFeishuDrive, LegacyAccessState: "hold"}
		require.NoError(t, db.Create(source).Error)
		log := &types.SyncLog{ID: id, DataSourceID: id, TenantID: 1, Status: types.SyncLogStatusRunning}
		if used {
			require.NoError(t, db.Create(log).Error)
		}
		require.NoError(t, NewDataSourceRepository(db).Delete(context.Background(), id))
		var stored types.DataSource
		require.NoError(t, db.Unscoped().First(&stored, "id=?", id).Error)
		require.True(t, stored.DeletedAt.Valid)
		want := "unmanaged"
		if used {
			want = "revoked"
		}
		require.Equal(t, want, stored.LegacyAccessState)
		_, claimed, err := (&SyncLogRepository{db: db}).ClaimExecution(context.Background(), id, log.ID, 1)
		require.NoError(t, err)
		require.False(t, claimed)
	}
}

func TestDeleteUnusedFeishuSourceRollsBackOnFailure(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges(metadata TEXT);
CREATE TABLE feishu_legacy_resources(data_source_id TEXT);
CREATE TABLE wiki_pages(knowledge_base_id TEXT);
CREATE TRIGGER prevent_delete BEFORE UPDATE OF deleted_at ON data_sources
BEGIN SELECT RAISE(FAIL,'delete refused'); END;`).Error)
	source := &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: "kb",
		Type: types.ConnectorTypeFeishu, LegacyAccessState: "hold"}
	require.NoError(t, db.Create(source).Error)
	db = types.EnableLegacyReadGuards(db)
	require.ErrorContains(t, NewDataSourceRepository(db).Delete(context.Background(), source.ID), "delete refused")
	var stored types.DataSource
	require.NoError(t, db.First(&stored, "id=?", source.ID).Error)
	require.False(t, stored.DeletedAt.Valid)
	require.Equal(t, "hold", stored.LegacyAccessState, "failed deletion cannot alter access")
}

func TestDeletedLegacySourceKeepsSyncMetadataRevoked(t *testing.T) {
	now := time.Now()
	for _, source := range []*types.DataSource{
		{LastSyncAt: &now},
		{LastSyncCursor: types.JSON(`{"page":1}`)},
		{LastSyncResult: types.JSON(`{}`)},
	} {
		state, err := deletedLegacySourceState(nil, source)
		require.NoError(t, err)
		require.Equal(t, "revoked", state)
	}
}
