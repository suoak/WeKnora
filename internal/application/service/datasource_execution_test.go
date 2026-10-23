package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type syncExecutionQueue struct{ calls atomic.Int64 }

func (q *syncExecutionQueue) Enqueue(_ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls.Add(1)
	return &asynq.TaskInfo{ID: uuid.NewString()}, nil
}

type syncExecutionConnector struct {
	deletedItemConnector
	calls        atomic.Int64
	entered      chan struct{}
	proceed      chan struct{}
	fetchErr     error
	panicOnFetch bool
}

func (c *syncExecutionConnector) FetchAll(
	ctx context.Context, _ *types.DataSourceConfig, _ []string,
) ([]types.FetchedItem, error) {
	c.calls.Add(1)
	if c.panicOnFetch {
		panic("injected sync worker panic")
	}
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	select {
	case c.entered <- struct{}{}:
	default:
	}
	select {
	case <-c.proceed:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSyncServiceSQLite(t *testing.T)   { syncServiceSuite(t, "sqlite") }
func TestSyncServicePostgres(t *testing.T) { syncServiceSuite(t, "postgres") }

func syncServiceSuite(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	dsRepo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	makeSvc := func() (*DataSourceService, *types.DataSource, *syncExecutionQueue, *syncExecutionConnector) {
		config, err := (&types.DataSourceConfig{Type: deletedItemConnectorType}).ToJSON()
		require.NoError(t, err)
		ds := &types.DataSource{
			ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Name: "sync", Type: deletedItemConnectorType,
			Status: types.DataSourceStatusActive, Config: config, SyncMode: types.SyncModeFull,
		}
		require.NoError(t, dsRepo.Create(context.Background(), ds))
		queue := &syncExecutionQueue{}
		connector := &syncExecutionConnector{entered: make(chan struct{}, 2), proceed: make(chan struct{})}
		registry := datasource.NewConnectorRegistry()
		require.NoError(t, registry.Register(connector))
		svc := &DataSourceService{
			dsRepo: dsRepo, syncLogRepo: logs, taskEnqueuer: queue, connectorRegistry: registry,
			kbService:  &processSyncKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
			tenantRepo: &processSyncTenantRepo{tenant: &types.Tenant{ID: 1}}, tagService: &processSyncTagService{},
		}
		return svc, ds, queue, connector
	}
	t.Run("source_failure_keeps_other_sources_admitted_P0", func(t *testing.T) {
		t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close(); feishusafety.InstallDefault(nil) })
		policy := feishusafety.DefaultConfig()
		policy.Namespace = uuid.NewString()
		guard, err := feishusafety.New(client, policy)
		require.NoError(t, err)
		ctx := feishusafety.WithScope(context.Background(), "marked-test-source")
		require.NoError(t, client.Set(ctx, guard.PermitKey(), guard.Fingerprint(), time.Minute).Err())
		feishusafety.InstallDefault(guard)
		svc, ds, _, connector := makeSvc()
		connector.fetchErr = errors.New("source HTTP 403: existing identity cannot read document")
		log, err := svc.ManualSync(ctx, ds.ID)
		require.NoError(t, err)
		body, err := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1})
		require.NoError(t, err)
		require.Error(t, svc.ProcessSync(ctx, asynq.NewTask(types.TypeDataSourceSync, body)))
		require.NoError(t, guard.Check(ctx), "ordinary source failure must not trip shared egress")
		otherSvc, otherDS, _, otherConnector := makeSvc()
		close(otherConnector.proceed)
		otherLog, err := otherSvc.ManualSync(ctx, otherDS.ID)
		require.NoError(t, err)
		body, err = json.Marshal(types.DataSourceSyncPayload{
			DataSourceID: otherDS.ID, SyncLogID: otherLog.ID, TenantID: 1,
		})
		require.NoError(t, err)
		require.NoError(t, otherSvc.ProcessSync(ctx, asynq.NewTask(types.TypeDataSourceSync, body)))
		finished, err := logs.FindByID(ctx, otherLog.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusSuccess, finished.Status)
		require.NoError(t, guard.Check(ctx))
	})
	t.Run("panic_final_P0", func(t *testing.T) {
		svc, ds, _, connector := makeSvc()
		connector.panicOnFetch = true
		log, err := svc.ManualSync(context.Background(), ds.ID)
		require.NoError(t, err)
		payload, _ := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1})
		require.ErrorContains(t,
			svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload)), "panicked")
		stored, err := logs.FindByID(context.Background(), log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusFailed, stored.Status)
		require.False(t, stored.ExecutionClaimed)
		_, err = svc.ManualSync(context.Background(), ds.ID)
		require.NoError(t, err, "failure cannot permanently block admission")
	})
	t.Run("pause_after_enqueue_P0", func(t *testing.T) {
		svc, ds, _, connector := makeSvc()
		ds.SyncSchedule = "0 0 0 * * *"
		require.NoError(t, dsRepo.Update(context.Background(), ds))
		log, err := svc.ManualSync(context.Background(), ds.ID)
		require.NoError(t, err)
		ds.Status = types.DataSourceStatusPaused
		require.NoError(t, dsRepo.Update(context.Background(), ds))
		payload, _ := json.Marshal(types.DataSourceSyncPayload{
			DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, Trigger: "schedule",
		})
		require.NoError(t, svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload)))
		require.Zero(t, connector.calls.Load())
		stored, err := logs.FindByID(context.Background(), log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusCanceled, stored.Status)
	})
	for _, recoverOnRetry := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure_retry_P0/recover%t", recoverOnRetry), func(t *testing.T) {
			svc, ds, _, connector := makeSvc()
			connector.fetchErr = context.DeadlineExceeded
			log, err := svc.ManualSync(context.Background(), ds.ID)
			require.NoError(t, err)
			payload, _ := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1})
			task := asynq.NewTask(types.TypeDataSourceSync, payload)
			require.Error(t, svc.ProcessSync(types.WithTaskRetryMetadata(context.Background(), 0, 1), task))
			waiting, err := logs.FindByID(context.Background(), log.ID)
			require.NoError(t, err)
			require.Equal(t, types.SyncLogStatusRunning, waiting.Status)
			require.False(t, waiting.ExecutionClaimed)
			if recoverOnRetry {
				connector.fetchErr = nil
				close(connector.proceed)
			}
			err = svc.ProcessSync(types.WithTaskRetryMetadata(context.Background(), 1, 1), task)
			if recoverOnRetry {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			finished, err := logs.FindByID(context.Background(), log.ID)
			require.NoError(t, err)
			require.False(t, finished.ExecutionClaimed)
			require.Greater(t, finished.ExecutionGeneration, waiting.ExecutionGeneration)
			if recoverOnRetry {
				require.Equal(t, types.SyncLogStatusSuccess, finished.Status)
			} else {
				require.Equal(t, types.SyncLogStatusFailed, finished.Status)
			}
		})
	}
	t.Run("manual_20", func(t *testing.T) {
		svc, ds, queue, _ := makeSvc()
		results := make(chan error, 20)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := svc.ManualSync(context.Background(), ds.ID); results <- err }()
		}
		wg.Wait()
		close(results)
		winners := 0
		for err := range results {
			if err == nil {
				winners++
			} else {
				require.ErrorIs(t, err, datasource.ErrSyncAlreadyRunning)
			}
		}
		require.Equal(t, 1, winners)
		require.EqualValues(t, 1, queue.calls.Load())
	})
	t.Run("duplicate_delivery_skips_connector", func(t *testing.T) {
		svc, ds, _, connector := makeSvc()
		log, err := svc.ManualSync(context.Background(), ds.ID)
		require.NoError(t, err)
		payload, err := json.Marshal(types.DataSourceSyncPayload{
			DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, ForceFull: true,
		})
		require.NoError(t, err)
		task := asynq.NewTask(types.TypeDataSourceSync, payload)
		winner := make(chan error, 1)
		go func() { winner <- svc.ProcessSync(context.Background(), task) }()
		select {
		case <-connector.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("winner did not enter connector")
		}
		require.NoError(t, svc.ProcessSync(context.Background(), task))
		require.EqualValues(t, 1, connector.calls.Load())
		close(connector.proceed)
		require.NoError(t, <-winner)
		stored, err := logs.FindByID(context.Background(), log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusSuccess, stored.Status)
		require.False(t, stored.ExecutionClaimed)
	})
	t.Run("manual_and_live_scheduler", func(t *testing.T) {
		svc, ds, queue, _ := makeSvc()
		ds.SyncSchedule = "* * * * * *"
		require.NoError(t, dsRepo.Update(context.Background(), ds))
		scheduler := datasource.NewScheduler(dsRepo, logs, queue)
		require.NoError(t, scheduler.Start(context.Background()))
		defer scheduler.Stop()
		// Align the actual manual entry with the next real cron tick. Either
		// order is legal; both paths must share one DB admission decision.
		next := time.Now().Truncate(time.Second).Add(time.Second)
		time.Sleep(time.Until(next))
		_, err := svc.ManualSync(context.Background(), ds.ID)
		if err != nil {
			require.ErrorIs(t, err, datasource.ErrSyncAlreadyRunning)
		}
		time.Sleep(100 * time.Millisecond)
		stored, err := logs.FindByDataSource(context.Background(), ds.ID, 100, 0)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.EqualValues(t, 1, queue.calls.Load())
	})
	t.Run("different_datasources_execute_concurrently", func(t *testing.T) {
		firstSvc, firstDS, _, firstConnector := makeSvc()
		secondSvc, secondDS, _, secondConnector := makeSvc()
		results := make(chan error, 2)
		for _, entry := range []struct {
			svc *DataSourceService
			ds  *types.DataSource
		}{{firstSvc, firstDS}, {secondSvc, secondDS}} {
			log, err := entry.svc.ManualSync(context.Background(), entry.ds.ID)
			require.NoError(t, err)
			payload, err := json.Marshal(types.DataSourceSyncPayload{
				DataSourceID: entry.ds.ID, TenantID: 1, SyncLogID: log.ID, ForceFull: true,
			})
			require.NoError(t, err)
			go func(svc *DataSourceService, payload []byte) {
				results <- svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))
			}(entry.svc, payload)
		}
		for _, connector := range []*syncExecutionConnector{firstConnector, secondConnector} {
			select {
			case <-connector.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("different datasource was serialized")
			}
		}
		close(firstConnector.proceed)
		close(secondConnector.proceed)
		require.NoError(t, <-results)
		require.NoError(t, <-results)
	})
	t.Run("timeout_release_and_new_generation", func(t *testing.T) {
		svc, ds, _, connector := makeSvc()
		log, err := svc.ManualSync(context.Background(), ds.ID)
		require.NoError(t, err)
		payload, err := json.Marshal(types.DataSourceSyncPayload{
			DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, ForceFull: true,
		})
		require.NoError(t, err)
		task := asynq.NewTask(types.TypeDataSourceSync, payload)
		ctx, cancel := context.WithCancel(types.WithTaskRetryMetadata(context.Background(), 0, 2))
		result := make(chan error, 1)
		go func() { result <- svc.ProcessSync(ctx, task) }()
		select {
		case <-connector.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("attempt did not start")
		}
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
		first, err := logs.FindByID(context.Background(), log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusRunning, first.Status)
		require.False(t, first.ExecutionClaimed)
		close(connector.proceed)
		require.NoError(t, svc.ProcessSync(types.WithTaskRetryMetadata(context.Background(), 1, 2), task))
		second, err := logs.FindByID(context.Background(), log.ID)
		require.NoError(t, err)
		require.Equal(t, types.SyncLogStatusSuccess, second.Status)
		require.Greater(t, second.ExecutionGeneration, first.ExecutionGeneration)
	})
}

func TestSyncTaskRetryMetadata(t *testing.T) {
	infra := errors.New("infrastructure failure")
	require.True(t, syncTaskCanRetry(types.WithTaskRetryMetadata(context.Background(), 0, 2), infra))
	require.False(t, syncTaskCanRetry(types.WithTaskRetryMetadata(context.Background(), 2, 2), infra))
	require.False(t, syncTaskCanRetry(types.WithTaskRetryMetadata(context.Background(), 0, 2),
		errors.Join(asynq.SkipRetry, infra)))
}

var _ interfaces.TaskEnqueuer = (*syncExecutionQueue)(nil)
