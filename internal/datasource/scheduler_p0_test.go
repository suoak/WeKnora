package datasource

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSyncScheduleSQLiteP0(t *testing.T)   { syncScheduleP0(t, "sqlite") }
func TestSyncSchedulePostgresP0(t *testing.T) { syncScheduleP0(t, "postgres") }
func syncScheduleP0(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	sources := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	var errorSource *types.DataSource
	for _, status := range []string{
		types.DataSourceStatusActive, types.DataSourceStatusError,
		types.DataSourceStatusPaused, types.DataSourceStatusDeleted,
	} {
		ds := &types.DataSource{
			ID:           uuid.NewString(),
			TenantID:     1,
			Status:       status,
			SyncSchedule: "0 0 0 * * *",
			Config:       types.JSON(`{}`),
		}
		require.NoError(t, sources.Create(ctx, ds))
		if status == types.DataSourceStatusError {
			errorSource = ds
		}
	}
	for range 2 {
		s := NewScheduler(sources, logs, &fakeTaskEnqueuer{})
		require.NoError(t, s.Start(ctx))
		require.Equal(t, 2, s.EntryCount(), "restart must register active/error, not paused/deleted")
		s.Stop()
	}
	queue := &fakeTaskEnqueuer{}
	s := NewScheduler(sources, logs, queue)
	old := time.Now().UTC().Add(-10 * time.Minute)
	failed := &types.SyncLog{
		DataSourceID: errorSource.ID,
		TenantID:     1,
		Status:       types.SyncLogStatusFailed,
		StartedAt:    old,
		FinishedAt:   &old,
	}
	require.NoError(t, logs.Create(ctx, failed))
	s.triggerSync(errorSource.ID, 1)
	require.EqualValues(t, 1, queue.count.Load(), "failed source must retry after cooldown")
	var running types.SyncLog
	require.NoError(
		t,
		db.Where("data_source_id=? AND status=?", errorSource.ID, types.SyncLogStatusRunning).First(&running).Error,
	)
	// Simulate success; a subsequent tick can run, without changing cron/config.
	running.Status = types.SyncLogStatusSuccess
	require.NoError(t, logs.Update(ctx, &running))
	s.triggerSync(errorSource.ID, 1)
	require.EqualValues(t, 2, queue.count.Load())
	var paused types.DataSource
	require.NoError(t, db.Where("status=?", types.DataSourceStatusPaused).First(&paused).Error)
	s.triggerSync(paused.ID, 1)
	require.EqualValues(t, 2, queue.count.Load())
}

func TestSyncRetryCooldownP0(t *testing.T) {
	now := time.Now().UTC()
	for streak := 1; streak <= 6; streak++ {
		history := make([]*types.SyncLog, streak)
		for i := range history {
			history[i] = &types.SyncLog{Status: types.SyncLogStatusFailed, FinishedAt: &now}
		}
		require.False(t, syncRetryDue(history, now.Add(time.Minute)))
		require.True(t, syncRetryDue(history, now.Add(time.Hour)))
		delay := min(5*time.Minute*time.Duration(1<<min(streak-1, 4)), time.Hour)
		require.False(t, syncRetryDue(history, now.Add(delay-time.Nanosecond)))
		require.True(t, syncRetryDue(history, now.Add(delay)))
	}
	require.True(t, syncRetryDue([]*types.SyncLog{{Status: types.SyncLogStatusSuccess}}, now))
}
