package feishusynccontrol

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutomaticServiceNativeSQLIdentityChangesBeforeFinalHealthStayClosed(t *testing.T) {
	for _, mode := range []string{"membership", "incarnation", "real_consumer_replacement"} {
		t.Run(mode, func(t *testing.T) {
			f, s, r, server, opt := renewableSQLServiceRuntime(t)
			ctx := context.Background()
			var queries atomic.Int32
			var injected atomic.Bool
			callback := "test:pre_renew_identity_" + uuid.NewString()
			require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if !strings.Contains(tx.Statement.SQL.String(), "system_settings") || queries.Add(1) != 4 {
					return
				}
				injected.Store(true)
				switch mode {
				case "membership":
					require.NoError(t, r.Incr(ctx, s.c.prefix+"membership_revision").Err())
				case "incarnation":
					require.NoError(t, r.HSet(ctx, s.c.prefix+"workers", s.c.Config.WorkerID,
						s.c.fingerprint+":"+uuid.NewString()).Err())
				case "real_consumer_replacement":
					server.Shutdown()
					startNativeAutomaticConsumer(t, opt)
					require.Eventually(t, func() bool {
						_, probeErr := s.health(ctx)
						return probeErr == nil
					}, time.Second, 5*time.Millisecond, "replacement must actually register in owned Redis")
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove(callback)) })
			require.Error(t, s.c.Step(ctx))
			require.True(t, injected.Load())
			require.Zero(t, s.stable.samples)
			require.Zero(t, r.Exists(ctx, s.c.Guard.PermitKey()).Val())
			require.Error(t, s.c.Guard.Check(ctx))
			if mode == "real_consumer_replacement" {
				require.Equal(t, "system_consumer_identity_conflict", r.Get(ctx, s.c.prefix+"trip").Val())
			}
		})
	}
}

func TestAutomaticServiceNativeSQLGuardedTaskAcrossAutonomousHeartbeats(t *testing.T) {
	workReady := make(chan context.Context, 1)
	finished := make(chan error, 1)
	var service *automaticService
	handler := asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		work := <-workReady
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			if err := service.c.Guard.Check(work); err != nil {
				finished <- err
				return err
			}
			select {
			case <-ctx.Done():
				finished <- ctx.Err()
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		finished <- nil
		return nil
	})
	f, s, r, _, opt := renewableSQLServiceRuntime(t, handler)
	service = s
	ctx := context.Background()
	logs := repository.NewSyncLogRepository(f.db)
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: f.source.ID,
		TenantID: f.tenant.ID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	created, err := logs.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, created)
	work, handled, err := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, handled)
	owner, claimed, err := logs.(interfaces.SyncExecutionRepository).
		ClaimExecution(work, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	work = feishusafety.WithRecoveryExecution(types.WithSyncExecution(work, owner), f.config, log.ID, owner.Generation)
	work, err = s.bindSource(work, f.source.ID, f.config)
	require.NoError(t, err)
	work = feishusafety.WithScope(work, f.source.ID)
	state, _, err := s.current(ctx)
	require.NoError(t, err)
	key := "asynq:servers:{" + state.WorkerID + ":" + state.ConsumerServerID + "}"
	score := r.ZScore(ctx, "asynq:servers", key).Val()
	// Controlled SQL latency belongs to the fixture, not a raised controller
	// timeout. Real Asynq heartbeats continue independently; no metadata writes.
	callback := "test:real_sql_latency_" + uuid.NewString()
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "system_settings") {
			time.Sleep(5 * time.Millisecond)
		}
	}))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove(callback)) })
	workReady <- work
	client := asynq.NewClient(opt)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	_, err = client.EnqueueContext(ctx, asynq.NewTask("fixture:guarded_heartbeat", nil),
		asynq.Queue(ApprovedSyncQueue), asynq.MaxRetry(0))
	require.NoError(t, err)
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case result := <-finished:
			require.NoError(t, result, "actual queue handler must keep its real source/generation authority")
			require.Greater(t, r.ZScore(ctx, "asynq:servers", key).Val(), score+5)
			require.Positive(t, r.PTTL(ctx, key).Val())
			return
		case <-deadline.C:
			t.Fatal("owned real Asynq handler did not complete in bounded time")
		default:
			require.NoError(t, s.c.Step(ctx), "every normal controller step must qualify without retry")
			time.Sleep(100 * time.Millisecond)
		}
	}
}
