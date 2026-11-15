package feishusafety

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDedicatedEmergencyPreservesSharedSyncAndPauseOwnership(t *testing.T) {
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	for _, marker := range []string{"", "operator_maintenance"} {
		t.Run(marker, func(t *testing.T) {
			g, r := fixture(t, Config{})
			ctx := context.Background()
			if marker != "" {
				require.NoError(t, r.Set(ctx, "asynq:{sync}:paused", marker, 0).Err())
			}
			require.NoError(t, r.RPush(ctx, "asynq:{sync}:pending", "gitlab-job").Err())
			require.NoError(t, EmergencyBlock(ctx, r, g.config.Namespace))
			require.Equal(t, marker, r.Get(ctx, "asynq:{sync}:paused").Val())
			require.Equal(t, []string{"gitlab-job"}, r.LRange(ctx, "asynq:{sync}:pending", 0, -1).Val())
			require.Equal(t, "feishu_emergency_stop", r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			require.Error(t, g.Check(ctx))
			require.NoError(t, r.Set(ctx, "asynq:{approved_sync}:paused", "operator_maintenance", 0).Err())
			require.NoError(t, EmergencyBlock(ctx, r, g.config.Namespace))
			require.Equal(t, "operator_maintenance", r.Get(ctx, "asynq:{approved_sync}:paused").Val())
		})
	}
}
