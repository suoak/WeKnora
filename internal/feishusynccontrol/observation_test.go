package feishusynccontrol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func observationFixture(t *testing.T) *normalFixture {
	t.Helper()
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	f.activateNormal(t)
	ctx := context.Background()
	// The real administrator revocation path is a separate prerequisite. The
	// reset implementation never writes SQL/Redis grants to fake this state.
	require.NoError(t, f.m.DeactivateNormalService(ctx))
	require.False(t, f.saved.Active)
	rows, err := f.r.XRevRangeN(ctx, f.c.prefix+"audit", "+", "-", 1).Result()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	f.p, err = f.m.Prepare(ctx, f.ids[0], "operator", RecoveryPrepareRequest{
		IncidentID: rows[0].ID, RuntimeSHA: f.c.Config.SourceSHA,
		Trip: f.r.Get(ctx, f.c.prefix+"trip").Val(),
	})
	require.NoError(t, err)
	require.True(t, f.p.HoldForReview)
	require.NotEmpty(t, f.p.NormalServiceSHA256)
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NoError(t, f.c.Step(ctx))
	return f
}

func observationCanary(t *testing.T, f *normalFixture) {
	t.Helper()
	require.NoError(t, f.m.Canary(context.Background(), f.p, f.approve(t, "canary"), "operator",
		func(ctx context.Context, bind func(string, int64) error) error {
			require.NoError(t, bind(uuid.NewString(), 1))
			require.NoError(t, f.c.Step(ctx))
			_, approved, err := BindApprovedSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
			require.Error(t, err)
			require.False(t, approved)
			return nil
		}))
}

func TestObservationCanaryRequiresSecondSignatureAndKeepsOtherSourcesClosed(t *testing.T) {
	f := observationFixture(t)
	ctx := context.Background()
	shared := snapshotSharedSync(t, f.r)
	observationCanary(t, f)
	require.NoError(t, f.c.Step(ctx))
	require.Equal(t, dedicatedRecoveryPause, f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	require.False(t, f.saved.Active, "the durable wide grant stays revoked, including on binary rollback")
	for _, id := range f.ids {
		_, _, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
		require.Error(t, err, "canary completion grants no periodic/ordinary admission")
	}
	_, err := f.m.PlanContinuity(ctx, "operator", f.ids[:1])
	require.Error(t, err, "legacy policy cannot discard the observation binding")
	_, err = f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids[:1], nil, 7)
	require.Error(t, err, "observation never resumes the unrelated shared queue")
	p, err := f.m.PlanSteady(ctx, "operator", f.ids[:1], 7)
	require.NoError(t, err)
	require.NotEmpty(t, p.ObservationPlanSHA256)
	bad := p
	bad.ManagedSources = append(append([]SteadySource(nil), p.ManagedSources...), SteadySource{})
	require.Error(t, f.m.ActivateSteady(ctx, bad, f.sign(t, p), "operator"))
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.NoError(t, f.c.Step(ctx))
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	work, approved, err := BindApprovedSource(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256)
	require.NoError(t, err)
	require.True(t, approved)
	require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, f.ids[0])))
	_, _, err = BindApprovedSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
	require.Error(t, err)
	InstallRecovery(f.m)
	t.Cleanup(func() { InstallRecovery(nil) })
	queued, err := QueueNormalSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
	require.NoError(t, err)
	require.False(t, queued, "producer must fall through to the exact signed binding")
	require.Equal(t, shared, snapshotSharedSync(t, f.r))
	require.False(t, f.saved.Active)
}

func TestObservationStateLossAndRootChangesFailClosed(t *testing.T) {
	for _, fault := range []string{
		"marker_missing", "mode_missing", "mode_invalid", "mode_wrong_type", "unknown_phase",
		"marker_wrong_type", "marker_changed", "root_missing", "root_reactivated", "root_wrong_type", "canceled",
	} {
		t.Run(fault, func(t *testing.T) {
			f := observationFixture(t)
			ctx := context.Background()
			switch fault {
			case "marker_missing":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"observation_required").Err())
			case "mode_missing":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"recovery").Err())
			case "mode_invalid":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", "{invalid", 0).Err())
			case "mode_wrong_type":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"recovery").Err())
				require.NoError(t, f.r.HSet(ctx, f.c.prefix+"recovery", "not", "a-mode").Err())
			case "unknown_phase":
				var state recoveryMode
				require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
				state.Phase = "unknown"
				raw, err := json.Marshal(state)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", raw, 0).Err())
			case "marker_wrong_type":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"observation_required").Err())
				require.NoError(t, f.r.HSet(ctx, f.c.prefix+"observation_required", "not", "a-marker").Err())
			case "marker_changed":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"observation_required", "mismatch", 0).Err())
			case "root_missing":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
			case "root_reactivated":
				root := *f.saved
				root.Active = true
				raw, err := json.Marshal(root)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", raw, 0).Err())
			case "root_wrong_type":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
				require.NoError(t, f.r.HSet(ctx, f.c.prefix+"normal_service", "not", "a-record").Err())
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			observing, err := f.m.observationBarrier(ctx)
			require.True(t, observing)
			require.Error(t, err)
			_, _, err = BindApprovedSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
			require.Error(t, err)
		})
	}
}

func TestObservationPrepareCannotOverrideActiveWideGrant(t *testing.T) {
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	f.activateNormal(t)
	ctx := context.Background()
	before := *f.saved
	_, err := f.m.Prepare(ctx, f.ids[0], "operator", RecoveryPrepareRequest{
		HoldForReview: true, RuntimeSHA: f.c.Config.SourceSHA,
		IncidentID: "not-authority", Trip: "not-authority",
	})
	require.Error(t, err)
	require.Equal(t, before, *f.saved)
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"observation_required").Val())
}

func TestObservationRejectsNonemptyDedicatedLaneWithoutRemovingTasks(t *testing.T) {
	f := newNormalFixture(t)
	f.c.Config.DedicatedSyncLane = true
	f.activateNormal(t)
	ctx := context.Background()
	require.NoError(t, f.m.DeactivateNormalService(ctx))
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", dedicatedRecoveryPause, 0).Err())
	require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", "retained-historical-task").Err())
	trip := f.r.Get(ctx, f.c.prefix+"trip").Val()
	_, err := f.m.Plan(ctx, f.ids[0], "operator")
	require.Error(t, err)
	pending := f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val()
	require.Equal(t, []string{"retained-historical-task"}, pending)
	require.Equal(t, trip, f.r.Get(ctx, f.c.prefix+"trip").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
}
