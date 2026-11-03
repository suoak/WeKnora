package feishusynccontrol

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSharedQueueReleaseDatabaseProofAndTerminalClaim(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}))
			kb := types.KnowledgeBase{ID: uuid.NewString(), TenantID: 1, Name: "isolated shared queue proof"}
			require.NoError(t, db.Create(&kb).Error)
			source := types.DataSource{
				ID: uuid.NewString(), KnowledgeBaseID: kb.ID, TenantID: 1,
				Type: "gitlab", Status: "active", LegacyAccessState: "unmanaged", Config: types.JSON(`{}`),
			}
			require.NoError(t, db.Create(&source).Error)
			ctx := context.Background()
			for _, status := range []string{"running", "success", "partial", "failed", "canceled", "deferred"} {
				log := types.SyncLog{ID: uuid.NewString(), DataSourceID: source.ID, TenantID: 1, Status: status}
				require.NoError(t, db.Create(&log).Error)
				p := types.DataSourceSyncPayload{DataSourceID: source.ID, SyncLogID: log.ID, TenantID: 1}
				proof, err := SharedQueueProofFromDatabase(ctx, db, p)
				require.NoError(t, err)
				if status == "running" {
					require.Equal(t, "execute_original", proof.Disposition)
				} else {
					require.Equal(t, "acknowledge_terminal", proof.Disposition)
					_, claimed, claimErr := repository.NewSyncLogRepository(db).(*repository.SyncLogRepository).
						ClaimExecution(ctx, source.ID, log.ID, 1)
					require.NoError(t, claimErr)
					require.False(t, claimed, "a released terminal delivery must never execute")
				}
			}
			faults := []string{"hold", "revoked", "paused", "feishu", "generation", "claimed", "tenant", "deleted"}
			for _, fault := range faults {
				t.Run(fault, func(t *testing.T) {
					candidate := source
					candidate.ID = uuid.NewString()
					log := types.SyncLog{
						ID: uuid.NewString(), DataSourceID: candidate.ID, TenantID: 1, Status: "running",
					}
					switch fault {
					case "hold", "revoked":
						candidate.LegacyAccessState = fault
					case "paused":
						candidate.Status = "paused"
					case "feishu":
						candidate.Type = "feishu_drive"
					case "generation":
						log.ExecutionGeneration = 1
					case "claimed":
						log.ExecutionClaimed = true
					}
					require.NoError(t, db.Create(&candidate).Error)
					require.NoError(t, db.Create(&log).Error)
					p := types.DataSourceSyncPayload{DataSourceID: candidate.ID, SyncLogID: log.ID, TenantID: 1}
					if fault == "tenant" {
						p.TenantID = 2
					}
					if fault == "deleted" {
						require.NoError(t, db.Delete(&candidate).Error)
					}
					_, err := SharedQueueProofFromDatabase(ctx, db, p)
					require.Error(t, err)
				})
			}
		})
	}
}

// The real Asynq consumer uses the same SQL Claim as ProcessSync. A released
// terminal delivery is acknowledged without entering connector work.
func TestSharedQueueReleaseRealConsumerKeepsTerminalRunsFenced(t *testing.T) {
	if os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR") == "" {
		t.Skip("requires disposable no-volume Redis")
	}
	f, proofs := sharedQueueFixture(t)
	ctx := context.Background()
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}))
	for _, p := range proofs {
		kb := types.KnowledgeBase{ID: uuid.NewString(), TenantID: p.TenantID, Name: "isolated queue consumer"}
		require.NoError(t, db.Create(&kb).Error)
		ds := types.DataSource{
			ID: p.SourceID, TenantID: p.TenantID, KnowledgeBaseID: kb.ID,
			Type: "gitlab", Status: "active", LegacyAccessState: "unmanaged", Config: types.JSON(`{}`),
		}
		require.NoError(t, db.Create(&ds).Error)
		log := types.SyncLog{
			ID: p.RunID, DataSourceID: p.SourceID, TenantID: p.TenantID,
			Status: p.Status, ExecutionGeneration: p.Generation,
		}
		require.NoError(t, db.Create(&log).Error)
	}
	f.m.ConfigureSharedQueueProof(func(
		ctx context.Context, p types.DataSourceSyncPayload,
	) (SharedQueueTaskProof, error) {
		return SharedQueueProofFromDatabase(ctx, db, p)
	})
	p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	var claimed, terminal atomic.Int32
	repo := repository.NewSyncLogRepository(db).(*repository.SyncLogRepository)
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDataSourceSync, func(ctx context.Context, task *asynq.Task) error {
		var payload types.DataSourceSyncPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		_, ok, err := repo.ClaimExecution(ctx, payload.DataSourceID, payload.SyncLogID, payload.TenantID)
		if err != nil {
			return err
		}
		if ok {
			claimed.Add(1)
		} else {
			terminal.Add(1)
		}
		return nil
	})
	server := asynq.NewServerFromRedisClient(f.r, asynq.Config{
		Concurrency:       1,
		Queues:            map[string]int{types.QueueSync: 1},
		TaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	require.NoError(t, server.Start(mux))
	defer server.Shutdown()
	require.Eventually(t, func() bool { return claimed.Load()+terminal.Load() == 6 },
		10*time.Second, 20*time.Millisecond)
	require.EqualValues(t, 2, claimed.Load())
	require.EqualValues(t, 4, terminal.Load())
	require.Eventually(t, func() bool {
		return f.r.LLen(ctx, "asynq:{sync}:pending").Val() == 0 &&
			f.r.LLen(ctx, "asynq:{sync}:active").Val() == 0
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, f.c.Step(ctx))
	for _, proof := range proofs {
		if proof.Status != "canceled" {
			continue
		}
		var log types.SyncLog
		require.NoError(t, db.Where("id=?", proof.RunID).Take(&log).Error)
		require.Equal(t, "canceled", log.Status)
		require.Equal(t, proof.Generation, log.ExecutionGeneration)
		require.False(t, log.ExecutionClaimed)
	}
	require.NotEmpty(t, f.r.Get(ctx, f.c.prefix+"shared_queue_checkpoint:"+p.SharedCheckpointID).Val())
}
