package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type generalSyncPending struct{ tasks []*asynq.TaskInfo }

func (p generalSyncPending) PendingSharedSyncDeliveries(context.Context) ([]*asynq.TaskInfo, error) {
	return p.tasks, nil
}

type generalSyncDeliveryQueue struct{ tasks []*asynq.Task }

func (q *generalSyncDeliveryQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{}, nil
}

func TestGeneralSyncRecoverySQLite(t *testing.T)   { testGeneralSyncRecovery(t, "sqlite") }
func TestGeneralSyncRecoveryPostgres(t *testing.T) { testGeneralSyncRecovery(t, "postgres") }

func testGeneralSyncRecovery(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	dsRepo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	for _, scenario := range []string{"pending", "feishu", "retried", "generation", "claimed", "terminal", "paused"} {
		t.Run(scenario, func(t *testing.T) {
			ds := &types.DataSource{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: deletedItemConnectorType,
				Status: types.DataSourceStatusActive, Config: types.JSON(`{}`),
				LastSyncCursor: types.JSON(`{"preserve":"cursor"}`),
			}
			if scenario == "feishu" {
				ds.Type = types.ConnectorTypeFeishuDrive
			}
			if scenario == "paused" {
				ds.Status = types.DataSourceStatusPaused
			}
			require.NoError(t, dsRepo.Create(context.Background(), ds))
			log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
			if scenario == "generation" {
				log.ExecutionGeneration = 1
			}
			if scenario == "claimed" {
				log.ExecutionClaimed = true
			}
			if scenario == "terminal" {
				log.Status = types.SyncLogStatusSuccess
			}
			require.NoError(t, logs.Create(context.Background(), log))
			body, err := json.Marshal(types.DataSourceSyncPayload{
				DataSourceID: ds.ID, TenantID: 1, SyncLogID: log.ID, Trigger: "original-cron", ForceFull: true,
			})
			require.NoError(t, err)
			original := &asynq.TaskInfo{
				ID: "original-task", Type: types.TypeDataSourceSync, Queue: types.QueueSync, Payload: body,
				Headers: map[string]string{"traceparent": "original-trace"}, State: asynq.TaskStatePending,
				MaxRetry: 0, Timeout: time.Minute,
			}
			if scenario == "retried" {
				original.Retried = 1
			}
			queue := &generalSyncDeliveryQueue{}
			n, err := RecoverGeneralSyncCapacity(context.Background(), db,
				generalSyncPending{[]*asynq.TaskInfo{original}}, queue)
			require.NoError(t, err)
			if scenario != "pending" {
				require.Zero(t, n)
				require.Empty(t, queue.tasks)
				return
			}
			require.Equal(t, 1, n)
			require.Equal(t, body, queue.tasks[0].Payload())
			require.Equal(t, original.Headers, queue.tasks[0].Headers())
			connector := &syncExecutionConnector{entered: make(chan struct{}, 2), proceed: make(chan struct{})}
			close(connector.proceed)
			registry := datasource.NewConnectorRegistry()
			require.NoError(t, registry.Register(connector))
			svc := &DataSourceService{
				dsRepo: dsRepo, syncLogRepo: logs, connectorRegistry: registry,
				kbService:  &processSyncKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
				tenantRepo: &processSyncTenantRepo{tenant: &types.Tenant{ID: 1}}, tagService: &processSyncTagService{},
			}
			require.NoError(t, svc.ProcessSync(types.WithGeneralSyncWorker(context.Background()), queue.tasks[0]))
			require.NoError(t, svc.ProcessSync(context.Background(), asynq.NewTask(original.Type, original.Payload)))
			require.EqualValues(t, 1, connector.calls.Load(), "original and reserved deliveries execute once")
			stored, err := logs.FindByID(context.Background(), log.ID)
			require.NoError(t, err)
			require.Equal(t, types.SyncLogStatusSuccess, stored.Status)
			require.EqualValues(t, 1, stored.ExecutionGeneration)
		})
	}
}

// Compile-time use ensures queue relaying retains the normal task interface.
var _ interfaces.TaskEnqueuer = (*generalSyncDeliveryQueue)(nil)
