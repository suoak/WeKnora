package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2PrivateSourceRestrictionRetainsExecutionFenceWithoutCredential(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	for _, ddl := range []string{
		"CREATE TABLE tenants(id BIGINT PRIMARY KEY,status TEXT,deleted_at TIMESTAMPTZ)",
		"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id BIGINT,deleted_at TIMESTAMPTZ)",
		"INSERT INTO tenants VALUES(1,'active',NULL)",
		"INSERT INTO knowledge_bases VALUES('kb',1,NULL)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	config := &types.DataSourceConfig{Settings: map[string]interface{}{
		"credential_reference": types.FeishuSourceReference{
			Version: 1, OwnerUserID: "A", Kind: types.FeishuUserOAuth,
			ConnectionID: "withdrawn", AppID: "app", IdentityVersion: "version",
		},
	}}
	blob, err := config.ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Name: "Controlled",
		Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive, Config: blob,
		LastSyncCursor: types.JSON(`{"accepted":"retained"}`),
	}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{
		ID: "run", DataSourceID: ds.ID, TenantID: ds.TenantID,
		Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, Generation: 1,
	})
	store := NewFeishuLegacyStore(db)
	require.NoError(t, store.SetSourceUncertain(ctx, ds, true), "a withdrawn credential must not prevent a restriction")
	var current types.DataSource
	require.NoError(t, db.Where("id = ?", ds.ID).First(&current).Error)
	require.Equal(t, "hold", current.LegacyAccessState)
	require.JSONEq(t, string(ds.LastSyncCursor), string(current.LastSyncCursor))
	var currentLog types.SyncLog
	require.NoError(t, db.Where("id = ?", log.ID).First(&currentLog).Error)
	require.EqualValues(t, 1, currentLog.ExecutionGeneration)
	require.True(t, currentLog.ExecutionClaimed)
	require.Error(t, store.SetSourceUncertain(ctx, ds, false),
		"restoring access still requires the live credential guard")
	stale := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, Generation: 2,
	})
	require.Error(t, store.SetSourceUncertain(stale, ds, true),
		"a restrictive write cannot bypass the claim generation")
}
