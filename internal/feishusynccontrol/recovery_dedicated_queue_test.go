package feishusynccontrol

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func dedicatedNormalRecoveryFixture(t *testing.T) *normalFixture {
	t.Helper()
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	f.activateNormal(t)
	ctx := context.Background()
	require.NoError(t, f.r.Del(ctx, "asynq:{sync}:paused").Err())
	require.NoError(t, f.r.RPush(ctx, "asynq:{sync}:pending", "gitlab-pending").Err())
	require.NoError(t, f.r.HSet(ctx, "asynq:{sync}:t:gitlab-pending",
		"msg", "independent work", "state", "pending").Err())
	require.NoError(t, f.r.ZAdd(ctx, "asynq:{sync}:retry", redis.Z{Score: 123, Member: "gitlab-retry"}).Err())
	require.NoError(t, f.r.ZAdd(ctx, "asynq:{sync}:scheduled", redis.Z{Score: 456, Member: "gitlab-scheduled"}).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("feishu-pending-%d", i)
		require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", id).Err())
		require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:"+id,
			"msg", []byte{byte(i), 0xff, 0, 1}, "state", "pending").Err())
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, f.r.ZAdd(ctx, "asynq:{approved_sync}:archived",
			redis.Z{Score: float64(i), Member: fmt.Sprintf("old-failure-%d", i)}).Err())
	}
	require.NoError(t, f.c.Guard.Trip(ctx, "embedding_headers_timeout"))
	var err error
	f.p, err = f.m.Plan(ctx, f.ids[0], "operator")
	require.NoError(t, err)
	require.Equal(t, "approved_sync", f.p.QueueName)
	require.Equal(t, 3, f.p.QueuePendingCount)
	return f
}

func snapshotSharedSync(t *testing.T, r *redis.Client) map[string]any {
	t.Helper()
	ctx := context.Background()
	keys, err := r.Keys(ctx, "asynq:{sync}:*").Result()
	require.NoError(t, err)
	out := map[string]any{}
	for _, key := range keys {
		kind, err := r.Type(ctx, key).Result()
		require.NoError(t, err)
		switch kind {
		case "string":
			out[key], err = r.Get(ctx, key).Result()
		case "list":
			out[key], err = r.LRange(ctx, key, 0, -1).Result()
		case "zset":
			out[key], err = r.ZRangeWithScores(ctx, key, 0, -1).Result()
		case "hash":
			out[key], err = r.HGetAll(ctx, key).Result()
		default:
			t.Fatalf("unexpected shared test key type %s", kind)
		}
		require.NoError(t, err)
	}
	return out
}

func TestDedicatedNormalIncidentRecoversWithoutPausingSharedSync(t *testing.T) {
	f := dedicatedNormalRecoveryFixture(t)
	ctx := context.Background()
	shared := snapshotSharedSync(t, f.r)
	archived := f.r.ZRangeWithScores(ctx, "asynq:{approved_sync}:archived", 0, -1).Val()
	before, err := f.m.incidentQueue(ctx)
	require.NoError(t, err)
	normal := *f.saved
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NoError(t, f.c.Step(ctx))
	require.Equal(t, shared, snapshotSharedSync(t, f.r))
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val(),
		"health cannot bypass the pending canary")
	require.NoError(t, f.m.Canary(ctx, f.p, f.approve(t, "canary"), "operator",
		func(work context.Context, bind func(string, int64) error) error {
			require.NoError(t, f.c.Step(work))
			require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			require.NoError(t, bind(uuid.NewString(), 1))
			require.NoError(t, f.c.Step(work))
			require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			return nil
		}))
	require.Equal(t, shared, snapshotSharedSync(t, f.r))
	require.Equal(t, archived, f.r.ZRangeWithScores(ctx, "asynq:{approved_sync}:archived", 0, -1).Val())
	require.Equal(t, before.ids, f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val())
	for i, id := range before.ids {
		require.Equal(t, before.fields[i], f.r.HGetAll(ctx, "asynq:{approved_sync}:t:"+id).Val())
	}
	require.Equal(t, normal, *f.saved, "the normal grant is neither disabled nor resigned")
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	status, err := f.m.Status(ctx, f.ids[0])
	require.NoError(t, err)
	require.Equal(t, "approved_sync", status["recovery_queue"])
	require.Equal(t, false, status["sync_paused"])
	require.Equal(t, true, status["permit_ready"])
	// A later stranded safety-owned pause can heal after the completed canary.
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	require.NoError(t, f.c.Step(ctx))
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	require.Equal(t, shared, snapshotSharedSync(t, f.r))
}

type dedicatedRecoveryCASClient struct {
	redis.UniversalClient
	mutate func()
}

func (c *dedicatedRecoveryCASClient) Eval(
	ctx context.Context, script string, keys []string, args ...interface{},
) *redis.Cmd {
	if c.mutate != nil && strings.Contains(script, "RECOVERY_INCIDENT_CHANGED") {
		mutate := c.mutate
		c.mutate = nil
		mutate()
	}
	return c.UniversalClient.Eval(ctx, script, keys, args...)
}

func TestDedicatedRecoveryCASRefusesChangedLaneWithoutChangingSharedWork(t *testing.T) {
	for _, fault := range []string{"pause owner", "pending bytes", "retry appeared"} {
		t.Run(fault, func(t *testing.T) {
			f := dedicatedNormalRecoveryFixture(t)
			ctx := context.Background()
			before := snapshotSharedSync(t, f.r)
			f.c.Client = &dedicatedRecoveryCASClient{UniversalClient: f.r, mutate: func() {
				switch fault {
				case "pause owner":
					require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "operator_maintenance", 0).Err())
				case "pending bytes":
					require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:feishu-pending-0",
						"msg", "changed").Err())
				case "retry appeared":
					require.NoError(t, f.r.ZAdd(ctx, "asynq:{approved_sync}:retry",
						redis.Z{Score: 1, Member: "new-retry"}).Err())
				}
			}}
			require.ErrorIs(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"), ErrRecoveryRejected)
			require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.Equal(t, before, snapshotSharedSync(t, f.r))
		})
	}
}

func TestDedicatedRecoveryRejectsNormalGrantRevocationAndOperatorPause(t *testing.T) {
	for _, fault := range []string{"revoked", "operator pause"} {
		t.Run(fault, func(t *testing.T) {
			f := dedicatedNormalRecoveryFixture(t)
			ctx := context.Background()
			before := snapshotSharedSync(t, f.r)
			if fault == "revoked" {
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
			} else {
				require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "operator_maintenance", 0).Err())
			}
			require.ErrorIs(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"), ErrRecoveryRejected)
			require.Equal(t, before, snapshotSharedSync(t, f.r))
		})
	}
}
