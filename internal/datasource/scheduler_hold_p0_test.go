package datasource

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSyncAccessHoldSQLiteP0(t *testing.T) { syncAccessHoldP0(t, "sqlite") }

func TestSyncAccessHoldPostgresP0(t *testing.T) { syncAccessHoldP0(t, "postgres") }

func syncAccessHoldP0(t *testing.T, dialect string) {
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", "all")
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	sources, logs := repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db)
	var initiallyActive []*types.DataSource
	for _, status := range []string{
		types.DataSourceStatusActive, types.DataSourceStatusError, types.DataSourceStatusPaused,
	} {
		for _, access := range []string{"active", "unmanaged", "hold", "revoked"} {
			ds := &types.DataSource{
				ID: uuid.NewString(), TenantID: 1, Type: types.ConnectorTypeFeishuDrive,
				Status: status, LegacyAccessState: access, Config: types.JSON(`{}`), SyncSchedule: "0 0 0 * * *",
			}
			require.NoError(t, sources.Create(ctx, ds))
			if status != types.DataSourceStatusPaused && (access == "active" || access == "unmanaged") {
				initiallyActive = append(initiallyActive, ds)
			}
		}
	}
	eligible, err := sources.FindActive(ctx)
	require.NoError(t, err)
	require.Len(t, eligible, 4, "repository must exclude held, revoked and paused sources")
	for range 2 {
		s := NewScheduler(sources, logs, &fakeTaskEnqueuer{})
		require.NoError(t, s.Start(ctx))
		require.Equal(t, 4, s.EntryCount(), "restart must not restore held schedules")
		s.Stop()
	}
	queue := &fakeTaskEnqueuer{}
	s := NewScheduler(sources, logs, queue)
	require.NoError(t, s.Start(ctx))
	defer s.Stop()
	for _, ds := range initiallyActive {
		// The cron already exists: a later access hold must be re-read at every tick.
		require.NoError(t, db.Model(ds).Update("legacy_access_state", "hold").Error)
		s.triggerSync(ds.ID, ds.TenantID)
		var saved types.DataSource
		require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
		require.Equal(t, "hold", saved.LegacyAccessState)
		require.Equal(t, ds.Status, saved.Status)
		require.Equal(t, ds.SyncSchedule, saved.SyncSchedule)
		require.Equal(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
	}
	require.Zero(t, queue.count.Load(), "existing cron ticks must not enqueue held sources")
}
