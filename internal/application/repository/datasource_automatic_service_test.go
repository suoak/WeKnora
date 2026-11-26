package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutomaticServiceClaimGuardAndPendingRequestRetention(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			kb := uuid.NewString()
			syncdb.LiveParent(t, db, 1, kb)
			ctx := context.Background()
			makeRun := func() (types.DataSource, types.SyncLog) {
				source := types.DataSource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: kb,
					Type: "feishu_drive", Status: "active", LegacyAccessState: "unmanaged", Config: types.JSON(`{}`),
				}
				require.NoError(t, db.Create(&source).Error)
				log := types.SyncLog{
					ID:           uuid.NewString(),
					DataSourceID: source.ID,
					TenantID:     1,
					Status:       types.SyncLogStatusRunning,
				}
				require.NoError(t, db.Create(&log).Error)
				return source, log
			}
			source, log := makeRun()
			queuedSource, queuedLog := makeRun()
			repo := &SyncLogRepository{db: db}
			changedEpoch := errors.New("verified system epoch changed")
			blocked := types.WithSyncAdmissionGuard(
				ctx,
				source.ID,
				log.ID,
				1,
				func(tx *gorm.DB, owner types.SyncExecution) error {
					var assigned types.SyncLog
					require.NoError(t, tx.First(&assigned, "id=?", owner.SyncLogID).Error)
					require.True(t, assigned.ExecutionClaimed)
					require.Equal(t, assigned.ExecutionGeneration, owner.Generation)
					return changedEpoch
				},
			)
			_, claimed, err := repo.ClaimExecution(blocked, source.ID, log.ID, 1)
			require.ErrorIs(t, err, changedEpoch)
			require.False(t, claimed)
			var unchanged types.SyncLog
			require.NoError(t, db.First(&unchanged, "id=?", log.ID).Error)
			require.False(t, unchanged.ExecutionClaimed)
			require.Zero(
				t,
				unchanged.ExecutionGeneration,
				"failed admission rolls back the actual generation assignment",
			)
			wrongRun := types.WithSyncAdmissionGuard(
				ctx,
				source.ID,
				uuid.NewString(),
				1,
				func(*gorm.DB, types.SyncExecution) error {
					t.Fatal("wrong-bound admission callback invoked")
					return nil
				},
			)
			_, claimed, err = repo.ClaimExecution(wrongRun, source.ID, log.ID, 1)
			require.ErrorIs(t, err, types.ErrStaleSyncExecution)
			require.False(t, claimed)
			owner, claimed, err := repo.ClaimExecution(ctx, source.ID, log.ID, 1)
			require.NoError(t, err)
			require.True(t, claimed)
			count, err := FenceClaimedLegacySyncExecutions(ctx, db)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			var fenced, pending types.SyncLog
			require.NoError(t, db.First(&fenced, "id=?", log.ID).Error)
			require.NoError(t, db.First(&pending, "id=?", queuedLog.ID).Error)
			require.Equal(t, types.SyncLogStatusCanceled, fenced.Status)
			require.False(t, fenced.ExecutionClaimed)
			require.Greater(t, fenced.ExecutionGeneration, owner.Generation)
			require.Equal(t, types.SyncLogStatusRunning, pending.Status)
			require.False(t, pending.ExecutionClaimed)
			require.Zero(t, pending.ExecutionGeneration)
			_, claimed, err = repo.ClaimExecution(ctx, queuedSource.ID, queuedLog.ID, 1)
			require.NoError(t, err)
			require.True(t, claimed, "accepted pending work remains eligible after the transient outage")
		})
	}
}
