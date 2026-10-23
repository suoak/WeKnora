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
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestLegacyFeishuScopeSQLiteP0(t *testing.T)   { legacyFeishuScopeP0(t, "sqlite") }
func TestLegacyFeishuScopePostgresP0(t *testing.T) { legacyFeishuScopeP0(t, "postgres") }

func legacyFeishuScopeP0(t *testing.T, dialect string) {
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", uuid.NewString())
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	sources, logs := repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db)
	for _, status := range []string{types.DataSourceStatusActive, types.DataSourceStatusError, types.DataSourceStatusPaused} {
		for _, trigger := range []string{"manual", "schedule"} {
			t.Run(status+"/"+trigger, func(t *testing.T) {
				stamp := time.Now().UTC().Truncate(time.Second)
				ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "scope-kb", Type: types.ConnectorTypeFeishuDrive,
					Status: status, SyncSchedule: "0 0 */6 * * *", Config: types.JSON(`{"type":"feishu_drive"}`), ErrorMessage: "original health",
					LastSyncAt: &stamp, LastSyncCursor: types.JSON(`{"page":"old"}`), LastSyncResult: types.JSON(`{"created":7}`)}
				require.NoError(t, sources.Create(ctx, ds))
				q := &syncExecutionQueue{}
				// No KB service or connector: any external/content access would fail.
				svc := &DataSourceService{dsRepo: sources, syncLogRepo: logs, taskEnqueuer: q}
				_, err := svc.ManualSync(ctx, ds.ID)
				require.ErrorIs(t, err, datasource.ErrLegacyFeishuSourceNotAllowed)
				var count int64
				require.NoError(t, db.Model(&types.SyncLog{}).Where("data_source_id = ?", ds.ID).Count(&count).Error)
				require.Zero(t, count)
				log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning, StartedAt: stamp}
				require.NoError(t, logs.Create(ctx, log))
				body, err := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, TenantID: 1, SyncLogID: log.ID, Trigger: trigger, ForceFull: true})
				require.NoError(t, err)
				task := asynq.NewTask(types.TypeDataSourceSync, body)
				require.NoError(t, svc.ProcessSync(ctx, task))
				require.NoError(t, svc.ProcessSync(ctx, task), "duplicate canceled delivery is harmless")
				savedLog, err := logs.FindByID(ctx, log.ID)
				require.NoError(t, err)
				require.Equal(t, types.SyncLogStatusCanceled, savedLog.Status)
				require.False(t, savedLog.ExecutionClaimed)
				require.NotNil(t, savedLog.FinishedAt)
				require.Contains(t, savedLog.ErrorMessage, "allowlist")
				saved, err := sources.FindByID(ctx, ds.ID)
				require.NoError(t, err)
				require.Equal(t, ds.Status, saved.Status)
				require.Equal(t, ds.ErrorMessage, saved.ErrorMessage)
				require.Equal(t, ds.SyncSchedule, saved.SyncSchedule)
				require.JSONEq(t, string(ds.Config), string(saved.Config))
				require.JSONEq(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
				require.JSONEq(t, string(ds.LastSyncResult), string(saved.LastSyncResult))
				require.True(t, ds.LastSyncAt.Equal(*saved.LastSyncAt))
				require.Zero(t, q.calls.Load())
			})
		}
	}
}
