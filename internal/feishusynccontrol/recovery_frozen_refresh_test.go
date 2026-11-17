package feishusynccontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestFrozenRecoveryRefreshHandlesBoundedSourceBacklog(t *testing.T) {
	f := frozenRefreshFixture(t)
	ctx := context.Background()
	for i := range 100 {
		id := fmt.Sprintf("new-source-sync-%d", i)
		require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", id).Err())
		require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:"+id, "msg", []byte{0xff, byte(i)}).Err())
	}
	var err error
	f.p, err = f.m.Plan(ctx, f.ids[0], "operator")
	require.NoError(t, err)
	require.Equal(t, 103, f.p.QueuePendingCount)
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.EqualValues(t, 103, f.r.LLen(ctx, "asynq:{approved_sync}:pending").Val())
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	for i := 103; i <= dedicatedRecoveryMaxPending; i++ {
		require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", fmt.Sprint(i)).Err())
	}
	_, err = f.m.Plan(ctx, f.ids[0], "operator")
	require.ErrorIs(t, err, ErrRecoveryRejected, "the dedicated snapshot retains a hard memory/work bound")
}

func frozenRefreshFixture(t *testing.T) *normalFixture {
	t.Helper()
	f := dedicatedNormalRecoveryFixture(t)
	ctx := context.Background()
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NoError(t, f.c.Step(ctx))
	return f
}

func TestFrozenRecoveryRefreshNeedsFreshApprovalAndPreservesEveryTask(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "queue-grew", true: "approval-expired"}[expired], func(t *testing.T) {
			f := frozenRefreshFixture(t)
			ctx := context.Background()
			shared := snapshotSharedSync(t, f.r)
			normal := *f.saved
			old := f.p
			oldApproval := f.approve(t, "reset")
			require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", "new-user-sync").Err())
			require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:new-user-sync",
				"msg", []byte{0, 0xff, 2}, "state", "pending").Err())
			if expired {
				var state recoveryMode
				require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
				state.ExpiresMS = time.Now().Add(-time.Minute).UnixMilli()
				raw, err := json.Marshal(state)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", raw, 0).Err())
			}
			before, err := f.m.incidentQueue(ctx)
			require.NoError(t, err)
			f.p, err = f.m.Plan(ctx, f.ids[0], "operator")
			require.NoError(t, err)
			require.Equal(t, "refresh_frozen", f.p.ResetMode)
			require.NotEqual(t, old.PlanID, f.p.PlanID)
			require.NotEqual(t, old.QueueSHA256, f.p.QueueSHA256)
			require.ErrorIs(t, f.m.Reset(ctx, f.p, oldApproval, "operator"), ErrRecoveryRejected)
			incarnation := f.c.incarnation
			require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
			require.Equal(t, incarnation, f.c.incarnation, "refresh does not replace the healthy guard")
			require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
			require.Equal(t, normal, *f.saved)
			require.Equal(t, shared, snapshotSharedSync(t, f.r))
			require.NoError(t, f.m.Canary(ctx, f.p, f.approve(t, "canary"), "operator",
				func(work context.Context, bind func(string, int64) error) error {
					require.NoError(t, f.c.Step(work))
					return bind(uuid.NewString(), 1)
				}))
			require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
			require.Equal(t, before.ids, f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val())
			for i, id := range before.ids {
				require.Equal(t, before.fields[i], f.r.HGetAll(ctx, "asynq:{approved_sync}:t:"+id).Val())
			}
			require.EqualValues(t, 20, f.r.ZCard(ctx, "asynq:{approved_sync}:archived").Val())
			require.Equal(t, shared, snapshotSharedSync(t, f.r))
		})
	}
}

type frozenRefreshCASClient struct {
	redis.UniversalClient
	mutate func()
}

func (c *frozenRefreshCASClient) Eval(
	ctx context.Context, script string, keys []string, args ...interface{},
) *redis.Cmd {
	if c.mutate != nil && strings.Contains(script, "RECOVERY_FROZEN_CHANGED") {
		mutate := c.mutate
		c.mutate = nil
		mutate()
	}
	return c.UniversalClient.Eval(ctx, script, keys, args...)
}

func TestFrozenRecoveryRefreshRejectsConcurrentChangesBeforeAnyTransition(t *testing.T) {
	for _, fault := range []string{"pending", "payload", "pause", "normal", "trip", "membership", "permit", "active"} {
		t.Run(fault, func(t *testing.T) {
			f := frozenRefreshFixture(t)
			ctx := context.Background()
			var err error
			f.p, err = f.m.Plan(ctx, f.ids[0], "operator")
			require.NoError(t, err)
			before := f.r.Get(ctx, f.c.prefix+"recovery").Val()
			shared := snapshotSharedSync(t, f.r)
			f.c.Client = &frozenRefreshCASClient{UniversalClient: f.r, mutate: func() {
				switch fault {
				case "pending":
					require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", "late-user-sync").Err())
				case "payload":
					require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:feishu-pending-0",
						"msg", "changed").Err())
				case "pause":
					require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "operator_pause", 0).Err())
				case "normal":
					require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
				case "trip":
					require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "new_incident", 0).Err())
				case "membership":
					require.NoError(t, f.r.Incr(ctx, f.c.prefix+"membership_revision").Err())
				case "permit":
					require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
				case "active":
					require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:active", "unexpected-run").Err())
				}
			}}
			require.ErrorIs(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"), ErrRecoveryRejected)
			require.Equal(t, before, f.r.Get(ctx, f.c.prefix+"recovery").Val())
			require.Equal(t, shared, snapshotSharedSync(t, f.r))
		})
	}
}
