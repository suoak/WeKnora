package datasource

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRC3LegacyFreezeConfiguration(t *testing.T) {
	for _, raw := range []string{"", "false", "0", "true", "1", "invalid", " true "} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", raw)
			want := raw != "" && raw != "false" && raw != "0"
			for _, kind := range []string{types.ConnectorTypeFeishu, types.ConnectorTypeFeishuDrive, types.ConnectorTypeLark, types.ConnectorTypeLarkDrive} {
				require.Equal(t, want, LegacyFeishuSyncSuspended(kind))
			}
			for _, kind := range []string{"feishu_personal", types.ConnectorTypeRSS, types.ConnectorTypeGitLab, ""} {
				require.False(t, LegacyFeishuSyncSuspended(kind))
			}
		})
	}
}

func TestRC3LegacyFreezeSchedulerAndStaleCallback(t *testing.T) {
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "true")
	repo := newFakeDataSourceRepo()
	logs := newFakeSyncLogRepo()
	// No enqueuer: stale callbacks must not reach queue dispatch.
	s := NewScheduler(repo, logs, nil)
	ds := &types.DataSource{ID: "legacy", TenantID: 1, Type: types.ConnectorTypeFeishu,
		Status: types.DataSourceStatusActive, SyncSchedule: "0 0 */6 * * *"}
	require.NoError(t, repo.Create(context.Background(), ds))
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(s.Stop)
	require.Zero(t, s.EntryCount())
	require.NoError(t, s.AddOrUpdate(ds))
	require.Zero(t, s.EntryCount())
	s.triggerSync(ds.ID, ds.TenantID)
	require.Empty(t, logs.logs)
}
