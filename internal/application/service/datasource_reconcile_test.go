package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type syncQueueProbe struct {
	live   map[string]bool
	err    error
	before func()
}

func TestSyncReconcilePaginationP0(t *testing.T) {
	db := syncdb.Open(t, "sqlite")
	old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	live := map[string]bool{}
	for i := 0; i < 201; i++ {
		ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, Status: types.DataSourceStatusActive, Config: types.JSON(`{}`)}
		require.NoError(t, db.Create(ds).Error)
		log := &types.SyncLog{ID: fmt.Sprintf("page-%03d", i), DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, StartedAt: old, UpdatedAt: old}
		require.NoError(t, db.Create(log).Error)
		if i < 200 {
			live[types.SyncTaskKey(1, ds.ID, log.ID)] = true
		}
	}
	n, err := ReconcileDataSourceSyncs(context.Background(), db, syncQueueProbe{live: live})
	require.NoError(t, err)
	require.Equal(t, 1, n, "a full page of live rows cannot starve a later orphan")
	var count int64
	require.NoError(t, db.Model(&types.SyncLog{}).Where("status=?", types.SyncLogStatusRunning).Count(&count).Error)
	require.EqualValues(t, 200, count)
}

func (p syncQueueProbe) LiveSyncTasks(context.Context) (map[string]bool, error) {
	if p.before != nil {
		p.before()
	}
	return p.live, p.err
}

func TestSyncReconcileSQLiteP0(t *testing.T)   { syncReconcileP0(t, "sqlite") }
func TestSyncReconcilePostgresP0(t *testing.T) { syncReconcileP0(t, "postgres") }
func syncReconcileP0(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	repo := repository.NewSyncLogRepository(db).(*repository.SyncLogRepository)
	for _, scenario := range []string{"archived", "absent", "legacy_null_heartbeat", "redis_error", "live_retry", "live_pending", "heartbeat_race", "new_generation", "paused", "newer_success"} {
		t.Run(scenario, func(t *testing.T) {
			ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, Status: types.DataSourceStatusActive, Config: types.JSON(`{}`), LastSyncCursor: types.JSON(`{"canary":"keep"}`)}
			if scenario == "paused" {
				ds.Status = types.DataSourceStatusPaused
			}
			require.NoError(t, db.Create(ds).Error)
			old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
			log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning, ExecutionGeneration: 6, ExecutionClaimed: true, StartedAt: old, UpdatedAt: old}
			require.NoError(t, db.Create(log).Error)
			if scenario == "legacy_null_heartbeat" {
				require.NoError(t, db.Model(log).UpdateColumn("updated_at", nil).Error)
			}
			key := types.SyncTaskKey(1, ds.ID, log.ID)
			probe := syncQueueProbe{live: map[string]bool{}}
			if scenario == "live_retry" || scenario == "live_pending" {
				probe.live[key] = true
			}
			if scenario == "redis_error" {
				probe.err = errors.New("redis unavailable")
			}
			owner := types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: 1, Generation: 6}
			if scenario == "heartbeat_race" {
				probe.before = func() {
					valid, err := repo.HeartbeatExecution(ctx, owner)
					require.NoError(t, err)
					require.True(t, valid)
				}
			}
			if scenario == "new_generation" {
				probe.before = func() {
					_, valid, err := repo.ClaimExecution(ctx, ds.ID, log.ID, 1)
					require.NoError(t, err)
					require.True(t, valid)
				}
			}
			if scenario == "newer_success" {
				require.NoError(t, db.Create(&types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusSuccess, StartedAt: time.Now().UTC()}).Error)
			}
			_, err := ReconcileDataSourceSyncs(ctx, db, probe)
			if scenario == "redis_error" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			got, err := repo.FindByID(ctx, log.ID)
			require.NoError(t, err)
			shouldFail := scenario == "archived" || scenario == "absent" || scenario == "legacy_null_heartbeat" || scenario == "paused" || scenario == "newer_success"
			if shouldFail {
				require.Equal(t, types.SyncLogStatusFailed, got.Status)
				require.False(t, got.ExecutionClaimed)
				require.EqualValues(t, 7, got.ExecutionGeneration)
				valid, err := repo.HeartbeatExecution(ctx, owner)
				require.NoError(t, err)
				require.False(t, valid)
				oldResult := *log
				oldResult.Status = types.SyncLogStatusSuccess
				valid, err = repo.WriteExecution(ctx, owner, ds, &oldResult, true)
				require.NoError(t, err)
				require.False(t, valid, "reconciled worker may not publish late success")
				before := got.UpdatedAt
				_, err = ReconcileDataSourceSyncs(ctx, db, probe)
				require.NoError(t, err)
				got, err = repo.FindByID(ctx, log.ID)
				require.NoError(t, err)
				require.Equal(t, before, got.UpdatedAt)
			} else {
				require.Equal(t, types.SyncLogStatusRunning, got.Status)
			}
			var current types.DataSource
			require.NoError(t, db.First(&current, "id=?", ds.ID).Error)
			require.JSONEq(t, string(ds.LastSyncCursor), string(current.LastSyncCursor))
			require.Nil(t, current.LastSyncAt)
			if scenario == "paused" {
				require.Equal(t, types.DataSourceStatusPaused, current.Status)
			}
			if scenario == "newer_success" {
				require.Equal(t, types.DataSourceStatusActive, current.Status)
			}
		})
	}
}
