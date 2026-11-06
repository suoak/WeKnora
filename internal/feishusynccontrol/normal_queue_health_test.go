package feishusynccontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalServiceHealthResumesOnlyItsSafetyPause(t *testing.T) {
	for _, marker := range []string{"feishu_safety_stop", "operator_pause", "wiki-global-drain:fixture"} {
		t.Run(marker, func(t *testing.T) {
			f := newNormalFixture(t)
			ctx := context.Background()
			f.activateNormal(t)
			grant := *f.saved
			incident := f.r.Get(ctx, f.c.prefix+"recovery").Val()
			require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", "retained_shared_queue", 0).Err())
			require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", marker, 0).Err())
			require.NoError(t, f.c.Step(ctx))
			if marker == "feishu_safety_stop" {
				require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
			} else {
				require.Equal(t, marker, f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			}
			require.Equal(t, "retained_shared_queue", f.r.Get(ctx, "asynq:{sync}:paused").Val())
			require.Equal(t, grant, *f.saved)
			require.Equal(t, incident, f.r.Get(ctx, f.c.prefix+"recovery").Val())
		})
	}
}

func TestNormalServiceFailedHealthPreservesOperatorPause(t *testing.T) {
	for _, failure := range []string{"trip", "revoked", "changed_record"} {
		t.Run(failure, func(t *testing.T) {
			f := newNormalFixture(t)
			ctx := context.Background()
			f.activateNormal(t)
			require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "operator_pause", 0).Err())
			switch failure {
			case "trip":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "actual_emergency", 0).Err())
			case "revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
			case "changed_record":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", "{}", 0).Err())
			}
			require.Error(t, f.c.Step(ctx))
			require.Equal(t, "operator_pause", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
		})
	}
}

func TestNormalServiceQueueWaitsForReplacementConsumerWithoutNewTrip(t *testing.T) {
	f := newNormalFixture(t)
	ctx := context.Background()
	f.activateNormal(t)
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	probe := f.c.ApprovedConsumers
	f.c.ApprovedConsumers = func(context.Context) ([]string, error) { return nil, nil }
	require.ErrorIs(t, f.c.Step(ctx), ErrNotReady)
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	f.c.ApprovedConsumers = probe
	require.NoError(t, f.c.Step(ctx))
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
}
