package feishusynccontrol

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestNormalServiceCanaryPreservesOperatorQueuePause(t *testing.T) {
	for _, reason := range []string{"1", "true", "operator_canary_review", "approved_sync_frozen"} {
		t.Run(reason, func(t *testing.T) {
			f := newNormalFixture(t)
			ctx := context.Background()
			f.activateNormal(t)
			require.NoError(t, f.r.Set(ctx, f.c.Guard.PermitKey(), f.c.Guard.Fingerprint(), time.Minute).Err())
			require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", reason, 0).Err())
			require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", "synthetic-preserved-job").Err())
			before := *f.saved
			audit := f.r.XLen(ctx, f.c.prefix+"control_audit").Val()
			require.NoError(t, f.m.resumeNormalQueueAfterHealth(ctx))
			require.NoError(t, f.m.resumeNormalQueueAfterCanary(ctx))
			require.Equal(t, reason, f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			require.Equal(t, []string{"synthetic-preserved-job"},
				f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val())
			require.Equal(t, audit, f.r.XLen(ctx, f.c.prefix+"control_audit").Val())
			require.Equal(t, before, *f.saved)
		})
	}
}

type normalPauseChangeHook struct {
	once   sync.Once
	change func()
}

func (h *normalPauseChangeHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *normalPauseChangeHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *normalPauseChangeHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "get" && cmd.Args()[1] == "asynq:{approved_sync}:paused" {
			h.once.Do(h.change)
		}
		return err
	}
}

func TestNormalServiceCanaryRejectsPauseOwnershipChangeBeforeResume(t *testing.T) {
	f := newNormalFixture(t)
	ctx := context.Background()
	f.activateNormal(t)
	require.NoError(t, f.r.Set(ctx, f.c.Guard.PermitKey(), f.c.Guard.Fingerprint(), time.Minute).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	before := *f.saved
	audit := f.r.XLen(ctx, f.c.prefix+"control_audit").Val()
	writer := redis.NewClient(f.r.Options())
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	fired := false
	f.r.AddHook(&normalPauseChangeHook{change: func() {
		fired = true
		require.NoError(t, writer.Set(ctx, "asynq:{approved_sync}:paused", "operator_canary_review", 0).Err())
	}})
	require.Error(t, f.m.resumeNormalQueueAfterCanary(ctx))
	require.True(t, fired)
	require.Equal(t, "operator_canary_review", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	require.Equal(t, audit, f.r.XLen(ctx, f.c.prefix+"control_audit").Val())
	require.Equal(t, before, *f.saved)
}
