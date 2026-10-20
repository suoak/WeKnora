package service

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestRC3LegacyFreezeSQLite(t *testing.T)   { rc3LegacyFreezeSuite(t, "sqlite") }
func TestRC3LegacyFreezePostgres(t *testing.T) { rc3LegacyFreezeSuite(t, "postgres") }

func rc3LegacyFreezeSuite(t *testing.T, dialect string) {
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "true")
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	repo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	fixture := func() (*DataSourceService, *types.DataSource, *syncExecutionQueue) {
		stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		ds := &types.DataSource{
			ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "rc3-kb", Type: types.ConnectorTypeFeishuDrive,
			Status: types.DataSourceStatusActive, SyncSchedule: "0 0 */6 * * *", LastSyncCursor: []byte(
				`{"token":"synthetic-checkpoint"}`,
			),
			LastSyncAt: &stamp, LastSyncResult: []byte(`{"created":7}`), Config: []byte(`{"type":"feishu_drive"}`),
		}
		require.NoError(t, repo.Create(ctx, ds))
		queue := &syncExecutionQueue{}
		return &DataSourceService{dsRepo: repo, syncLogRepo: logs, taskEnqueuer: queue}, ds, queue
	}
	unchanged := func(ds *types.DataSource) {
		saved, err := repo.FindByID(ctx, ds.ID)
		require.NoError(t, err)
		require.JSONEq(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
		require.JSONEq(t, string(ds.LastSyncResult), string(saved.LastSyncResult))
		require.True(t, ds.LastSyncAt.Equal(*saved.LastSyncAt))
		require.Equal(t, ds.KnowledgeBaseID, saved.KnowledgeBaseID)
		require.Equal(t, ds.TenantID, saved.TenantID)
	}
	t.Run("manual", func(t *testing.T) {
		s, ds, q := fixture()
		_, err := s.ManualSync(ctx, ds.ID)
		require.ErrorContains(t, err, "temporarily suspended")
		require.Zero(t, q.calls.Load())
		var count int64
		require.NoError(
			t,
			db.Model(&types.SyncLog{}).Where("data_source_id = ?", ds.ID).Count(&count).Error,
		)
		require.Zero(t, count)
		unchanged(ds)
	})
	t.Run("resume", func(t *testing.T) {
		s, ds, _ := fixture()
		ds.Status = types.DataSourceStatusPaused
		require.NoError(t, repo.Update(ctx, ds))
		require.ErrorContains(t, s.ResumeDataSource(ctx, ds.ID), "temporarily suspended")
		saved, err := repo.FindByID(ctx, ds.ID)
		require.NoError(t, err)
		require.Equal(t, types.DataSourceStatusPaused, saved.Status)
		unchanged(ds)
	})
	t.Run("update_cannot_change_type_to_bypass", func(t *testing.T) {
		s, ds, _ := fixture()
		changed := *ds
		changed.Type = types.ConnectorTypeRSS
		_, err := s.UpdateDataSource(ctx, &changed)
		require.ErrorContains(t, err, "temporarily suspended")
		unchanged(ds)
	})
	t.Run("create", func(t *testing.T) {
		s, ds, _ := fixture()
		candidate := *ds
		candidate.ID = uuid.NewString()
		_, err := s.CreateDataSource(ctx, &candidate)
		require.ErrorContains(t, err, "temporarily suspended")
		_, err = repo.FindByID(ctx, candidate.ID)
		require.Error(t, err)
	})
	t.Run("old_queued_task_replay", func(t *testing.T) {
		s, ds, q := fixture()
		log := &types.SyncLog{
			ID:           uuid.NewString(),
			TenantID:     ds.TenantID,
			DataSourceID: ds.ID,
			Status:       types.SyncLogStatusRunning,
			StartedAt:    time.Now(),
		}
		require.NoError(t, logs.Create(ctx, log))
		payload, err := json.Marshal(
			types.DataSourceSyncPayload{
				TenantID:     ds.TenantID,
				DataSourceID: ds.ID,
				SyncLogID:    log.ID,
				ForceFull:    true,
			},
		)
		require.NoError(t, err)
		task := asynq.NewTask(types.TypeDataSourceSync, payload)
		// No connector or knowledge service is installed: reaching either is a test failure.
		err = s.ProcessSync(ctx, task)
		require.ErrorIs(t, err, asynq.SkipRetry)
		require.ErrorContains(t, err, "temporarily suspended")
		savedLog, err := logs.FindByID(ctx, log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusFailed, savedLog.Status)
		require.False(t, savedLog.ExecutionClaimed)
		require.NotNil(t, savedLog.FinishedAt)
		require.NoError(t, s.ProcessSync(ctx, task))
		saved, err := repo.FindByID(ctx, ds.ID)
		require.NoError(t, err)
		require.Equal(t, types.DataSourceStatusPaused, saved.Status)
		require.Zero(t, q.calls.Load())
		unchanged(ds)
	})
	t.Run("concurrent_manual", func(t *testing.T) {
		s, ds, q := fixture()
		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := s.ManualSync(ctx, ds.ID); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.ErrorContains(t, err, "temporarily suspended")
		}
		require.Zero(t, q.calls.Load())
		unchanged(ds)
	})
}

func TestRC3LegacyFreezePostgresAsynq(t *testing.T) {
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "true")
	addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("REQUIRE_SYNC_EXECUTION_POSTGRES") == "1" {
			t.Fatal("real isolated Redis required")
		}
		t.Skip("real isolated Redis not configured")
	}
	db := syncdb.Open(t, "postgres")
	ctx := context.Background()
	repo, logs := repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "rc3-asynq-kb", Type: types.ConnectorTypeFeishu,
		Status: types.DataSourceStatusPaused, LastSyncCursor: []byte(
			`{"page":"synthetic-last-accepted"}`,
		), LastSyncResult: []byte(
			`{"created":3}`,
		),
	}
	require.NoError(t, repo.Create(ctx, ds))
	log := &types.SyncLog{
		ID:           uuid.NewString(),
		TenantID:     1,
		DataSourceID: ds.ID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now(),
	}
	require.NoError(t, logs.Create(ctx, log))
	svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs}
	opts := asynq.RedisClientOpt{Addr: addr}
	queue := "rc3-suspend-" + uuid.NewString()
	server := asynq.NewServer(
		opts,
		asynq.Config{Concurrency: 2, Queues: map[string]int{queue: 1}, ShutdownTimeout: time.Second},
	)
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDataSourceSync, svc.ProcessSync)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)
	client, inspector := asynq.NewClient(opts), asynq.NewInspector(opts)
	t.Cleanup(func() { _ = client.Close(); _ = inspector.Close() })
	payload, err := json.Marshal(
		types.DataSourceSyncPayload{TenantID: 1, DataSourceID: ds.ID, SyncLogID: log.ID},
	)
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = client.Enqueue(
		asynq.NewTask(types.TypeDataSourceSync, payload),
		asynq.Queue(queue),
		asynq.TaskID(id),
		asynq.MaxRetry(5),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := inspector.GetTaskInfo(queue, id)
		return err == nil && info.State == asynq.TaskStateArchived
	}, 15*time.Second, 50*time.Millisecond)
	info, err := inspector.GetTaskInfo(queue, id)
	require.NoError(t, err)
	require.Contains(t, info.LastErr, "temporarily suspended")
	require.JSONEq(t, string(payload), string(info.Payload))
	savedLog, err := logs.FindByID(ctx, log.ID)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusFailed, savedLog.Status)
	require.False(t, savedLog.ExecutionClaimed)
	saved, err := repo.FindByID(ctx, ds.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
	require.JSONEq(t, string(ds.LastSyncResult), string(saved.LastSyncResult))
	require.Equal(t, ds.KnowledgeBaseID, saved.KnowledgeBaseID)
	require.Equal(t, types.DataSourceStatusPaused, saved.Status)
	_, err = client.Enqueue(
		asynq.NewTask(types.TypeDataSourceSync, payload),
		asynq.Queue(queue),
		asynq.TaskID(id),
	)
	require.ErrorIs(t, err, asynq.ErrTaskIDConflict)
	// Remove only this test's unique archived task, never any shared queue data.
	require.NoError(t, inspector.DeleteTask(queue, id))
}
