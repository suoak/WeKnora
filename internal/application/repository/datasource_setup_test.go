package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEnabledSourceInitialSetupSQLite(t *testing.T) {
	enabledSourceInitialSetup(t, "sqlite")
}

func TestEnabledSourceInitialSetupPostgres(t *testing.T) {
	enabledSourceInitialSetup(t, "postgres")
}

func enabledSourceInitialSetup(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges(metadata JSONB);
CREATE TABLE feishu_legacy_resources(data_source_id TEXT);`).Error)
	repo := NewDataSourceRepository(types.EnableLegacyReadGuards(db))
	for _, mode := range []string{"new", "active_access", "pending_execution", "published_source"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			source := &types.DataSource{
				ID: mode, TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeFeishu,
				Status: types.DataSourceStatusActive, LegacyAccessState: "unmanaged",
				Config: types.JSON(`{"resource_ids":[]}`),
			}
			want := "unmanaged"
			if mode == "active_access" {
				source.LegacyAccessState, want = "active", "active"
			}
			require.NoError(t, repo.Create(ctx, source))
			switch mode {
			case "pending_execution":
				require.NoError(t, db.Create(&types.SyncLog{
					ID: mode, DataSourceID: source.ID, TenantID: 1, Status: types.SyncLogStatusRunning,
				}).Error)
				want = "hold"
			case "published_source":
				require.NoError(t, db.Exec("INSERT INTO feishu_legacy_resources(data_source_id) VALUES(?)", source.ID).Error)
				want = "hold"
			}
			// Initial credential/resource selection can involve more than one save.
			for _, config := range []string{`{"resource_ids":["root"]}`, `{"resource_ids":["final-root"]}`} {
				source.Config = types.JSON(config)
				require.NoError(t, repo.Update(ctx, source))
				stored, err := repo.FindByID(ctx, source.ID)
				require.NoError(t, err)
				require.Equal(t, want, stored.LegacyAccessState)
				require.Equal(t, types.DataSourceStatusActive, stored.Status)
				require.JSONEq(t, config, string(stored.Config))
			}
		})
	}
}
