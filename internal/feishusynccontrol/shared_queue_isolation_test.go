package feishusynccontrol

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestDedicatedFeishuStopDoesNotPauseSharedSync(t *testing.T) {
	for _, marker := range []string{"", "operator_maintenance"} {
		t.Run(marker, func(t *testing.T) {
			f := newControlFixture(t)
			f.config.DedicatedSyncLane = true
			c := f.controller(t, "worker-a", f.config.Scope)
			ctx := context.Background()
			if marker != "" {
				require.NoError(t, f.client.Set(ctx, "asynq:{sync}:paused", marker, 0).Err())
			}
			require.NoError(t, f.client.RPush(ctx, "asynq:{sync}:pending", "gitlab-job").Err())
			require.Error(t, c.EmergencyStop(ctx))
			require.Equal(t, marker, f.client.Get(ctx, "asynq:{sync}:paused").Val())
			require.Equal(t, []string{"gitlab-job"}, f.client.LRange(ctx, "asynq:{sync}:pending", 0, -1).Val())
			require.Equal(t, "feishu_safety_stop", f.client.Get(ctx, "asynq:{approved_sync}:paused").Val())
			require.EqualValues(t, 1, f.client.Exists(ctx, c.prefix+"trip").Val())
			require.Equal(t, 1, f.stops)
		})
	}
}

func TestNormalHealthRecoversLegacySharedFeishuPause(t *testing.T) {
	for _, marker := range []string{"feishu_safety_stop", "invalid_feishu_safety_configuration", "approved_sync_frozen", "feishu_emergency_stop", "operator_maintenance", "1"} {
		t.Run(marker, func(t *testing.T) {
			f := newNormalFixture(t)
			f.c.Config.DedicatedSyncLane = true
			ctx := context.Background()
			f.activateNormal(t)
			require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", marker, 0).Err())
			require.NoError(t, f.r.RPush(ctx, "asynq:{sync}:pending", "gitlab-job").Err())
			pending := f.r.LRange(ctx, "asynq:{sync}:pending", 0, -1).Val()
			// Recovery must work even when the dedicated lane is already open.
			require.NoError(t, f.r.Del(ctx, "asynq:{approved_sync}:paused").Err())
			require.NoError(t, f.c.Step(ctx))
			if marker == "feishu_safety_stop" || marker == "invalid_feishu_safety_configuration" || marker == "approved_sync_frozen" || marker == "feishu_emergency_stop" {
				require.Zero(t, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			} else {
				require.Equal(t, marker, f.r.Get(ctx, "asynq:{sync}:paused").Val())
			}
			require.Equal(t, pending, f.r.LRange(ctx, "asynq:{sync}:pending", 0, -1).Val())
		})
	}
}

func TestSharedPauseRecoveryRejectsUnsafeState(t *testing.T) {
	for _, failure := range []string{"trip", "missing_permit", "missing_consumer", "revoked", "changed_record", "legacy_lane"} {
		t.Run(failure, func(t *testing.T) {
			f := newNormalFixture(t)
			f.c.Config.DedicatedSyncLane = true
			ctx := context.Background()
			f.activateNormal(t)
			require.NoError(t, f.r.Set(ctx, f.c.Guard.PermitKey(), f.c.Guard.Fingerprint(), time.Minute).Err())
			require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err())
			switch failure {
			case "trip":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "emergency", 0).Err())
			case "missing_permit":
				require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
			case "missing_consumer":
				f.c.ApprovedConsumers = func(context.Context) ([]string, error) { return nil, nil }
			case "revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
			case "changed_record":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", "{}", 0).Err())
			case "legacy_lane":
				f.c.Config.DedicatedSyncLane = false
			}
			_ = f.m.resumeNormalQueueAfterHealth(ctx)
			require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{sync}:paused").Val())
		})
	}
}

func TestSharedPauseRecoveryPreservesConcurrentOperatorPause(t *testing.T) {
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	ctx := context.Background()
	f.activateNormal(t)
	require.NoError(t, f.r.Set(ctx, f.c.Guard.PermitKey(), f.c.Guard.Fingerprint(), time.Minute).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err())
	writer := redis.NewClient(f.r.Options())
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	fired := false
	f.r.AddHook(&normalPauseChangeHook{key: "asynq:{sync}:paused", change: func() {
		fired = true
		require.NoError(t, writer.Set(ctx, "asynq:{sync}:paused", "operator_maintenance", 0).Err())
	}})
	require.NoError(t, f.m.resumeNormalQueueAfterHealth(ctx))
	require.True(t, fired)
	require.Equal(t, "operator_maintenance", f.r.Get(ctx, "asynq:{sync}:paused").Val())
}

func TestSharedPauseRecoveryRequiresDurableAuditBeforeOpening(t *testing.T) {
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	ctx := context.Background()
	f.activateNormal(t)
	require.NoError(t, f.r.Set(ctx, f.c.Guard.PermitKey(), f.c.Guard.Fingerprint(), time.Minute).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err())
	require.NoError(t, f.r.Del(ctx, "asynq:{approved_sync}:paused", f.c.prefix+"control_audit").Err())
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"control_audit", "unavailable_stream", 0).Err())
	require.Error(t, f.m.resumeNormalQueueAfterHealth(ctx))
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{sync}:paused").Val())
}
