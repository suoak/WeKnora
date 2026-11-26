package feishusynccontrol

import (
	"context"
	"strings"
	"sync"
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

func TestAutomaticServiceNativeSQLGuardedTaskAcrossAuditedReadBoundaries(t *testing.T) {
	workReady := make(chan context.Context, 1)
	finished := make(chan error, 1)
	started, handlerDone := make(chan struct{}), make(chan struct{})
	var service *automaticService
	var fixtureMetadata sync.Mutex
	handler := asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		close(started)
		defer close(handlerDone)
		work := <-workReady
		taskWork, cancelWork := context.WithCancel(work)
		stopCancel := context.AfterFunc(ctx, cancelWork)
		defer stopCancel()
		defer cancelWork()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			fixtureMetadata.Lock()
			err := service.c.Guard.Check(taskWork)
			fixtureMetadata.Unlock()
			if err != nil {
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
	f, s, r, server, opt := renewableSQLServiceRuntime(t, handler)
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
	// These explicit SDK-equivalent encoding writes exercise the read boundary;
	// the real server independently advances its own heartbeat/registry score.
	probe := s.c.ApprovedConsumers
	lastInjected := time.Time{}
	injected := 0
	s.c.ApprovedConsumers = func(probeCtx context.Context) ([]string, error) {
		ids, probeErr := probe(probeCtx)
		if probeErr == nil && time.Since(lastInjected) >= 2*time.Second {
			// Coordinate ONLY explicit fixture writes with Guard's independent
			// request CAS. No request check is skipped; autonomous SDK heartbeats
			// remain uncoordinated. This test targets the controller read boundary.
			func() {
				fixtureMetadata.Lock()
				defer fixtureMetadata.Unlock()
				reorderActualConsumerMetadata(t, r, key)
			}()
			lastInjected = time.Now()
			injected++
		}
		return ids, probeErr
	}

	// Controlled SQL latency belongs to the fixture, not a raised controller
	// timeout. The SQL callback itself never writes Consumer metadata.
	callback := "test:real_sql_latency_" + uuid.NewString()
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "system_settings") {
			time.Sleep(5 * time.Millisecond)
		}
	}))
	t.Cleanup(func() {
		// A failed assertion must not remove/compile GORM callbacks while the
		// independently running handler can still execute a database read.
		server.Shutdown()
		select {
		case <-started:
			select {
			case <-handlerDone:
			case <-time.After(3 * time.Second):
				t.Error("owned handler did not stop; retain its callback rather than mutate it concurrently")
				return
			}
		default:
		}
		require.NoError(t, f.db.Callback().Query().Remove(callback))
	})
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
			require.GreaterOrEqual(
				t,
				injected,
				3,
				"the real guarded handler must span multiple audited read boundaries",
			)
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
