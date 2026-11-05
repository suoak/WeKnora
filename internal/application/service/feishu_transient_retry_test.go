package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestFeishuTransientRetryKeepsOriginalRunClaimAndCursor(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			ds := &types.DataSource{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu", Status: "active",
				LegacyAccessState: "active", LastSyncCursor: types.JSON(`{"checkpoint":"verified-page"}`),
			}
			require.NoError(t, db.Create(ds).Error)
			logs := repository.NewSyncLogRepository(db)
			log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
			created, err := logs.CreateIfNoRunning(context.Background(), log)
			require.NoError(t, err)
			require.True(t, created)
			executor := logs.(interfaces.SyncExecutionRepository)
			execution, claimed, err := executor.ClaimExecution(context.Background(), ds.ID, log.ID, 1)
			require.NoError(t, err)
			require.True(t, claimed)
			ctx := types.WithSyncExecution(context.Background(), execution)
			svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: logs}
			failure := &core.APIError{Category: core.ErrorCategoryTransient}
			err = svc.failSyncAttempt(ctx, ds, log, failure, "Temporary Feishu failure; automatically retrying")
			require.ErrorIs(t, err, failure)
			require.False(t, errors.Is(err, asynq.SkipRetry))
			require.NoError(t, svc.writeSyncExecution(ctx, ds, log, true), "normal worker tail releases the Claim")
			stored, err := logs.FindByID(context.Background(), log.ID)
			require.NoError(t, err)
			require.Equal(t, types.SyncLogStatusRunning, stored.Status)
			require.False(t, stored.ExecutionClaimed)
			next, reclaimed, err := executor.ClaimExecution(context.Background(), ds.ID, log.ID, 1)
			require.NoError(t, err)
			require.True(t, reclaimed)
			require.Equal(t, execution.Generation+1, next.Generation)
			fresh, err := svc.dsRepo.FindByID(context.Background(), ds.ID)
			require.NoError(t, err)
			require.JSONEq(t, string(ds.LastSyncCursor), string(fresh.LastSyncCursor))
			require.Equal(t, "active", fresh.LegacyAccessState)
		})
	}
}
