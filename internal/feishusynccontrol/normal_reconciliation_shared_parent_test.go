package feishusynccontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func normalSharedContinuationFixture(t *testing.T) (*normalFixture, string, string) {
	t.Helper()
	steady, _ := sharedQueueFixture(t)
	f := &normalFixture{steadyFixture: steady, sources: map[string]NormalSourceAuthority{}}
	require.NoError(t, f.m.ConfigureNormalService(f.load, f.store, nil))
	ctx := context.Background()
	plan, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 1)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, plan, f.sign(t, plan), "operator"))
	f.activateNormal(t)
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
	delta := (48 * time.Hour).Milliseconds()
	old := state.Steady
	old.Plan.CreatedMS -= delta
	old.Plan.ExpiresMS -= delta
	old.Plan.AuthorizationUntilMS -= delta
	old.ActivatedMS -= delta
	old.Plan.RuntimeSHA = strings.Repeat("0", 40)
	old.Plan.WorkerID = "departed-worker"
	old.Plan.Incarnation = uuid.NewString()
	old.Approval = f.sign(t, old.Plan)
	state.RuntimeSHA, state.WorkerID, state.Incarnation = old.Plan.RuntimeSHA, old.Plan.WorkerID, old.Plan.Incarnation
	state.ExpiresMS = old.Plan.AuthorizationUntilMS
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", raw, 0).Err())
	held := f.ids[0]
	facts := f.facts[held]
	facts.Eligible, facts.ReconciliationEligible = false, true
	f.facts[held] = facts
	authority := f.authority[held]
	authority.SourceStatus, authority.AccessState = "error", "hold"
	f.authority[held] = authority
	return f, held, f.c.prefix + "shared_queue_checkpoint:" + plan.SharedCheckpointID
}

func TestNormalContinuationInheritsVerifiedSharedCheckpointAfterWorkerReplacement(t *testing.T) {
	f, held, checkpointKey := normalSharedContinuationFixture(t)
	ctx := context.Background()
	root := *f.saved
	checkpoint := f.r.Get(ctx, checkpointKey).Val()
	queue, err := f.m.queue(ctx)
	require.NoError(t, err, "a verified normal root may inspect the historical checkpoint")
	plan, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1, 3*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, plan.Version)
	require.True(t, plan.ReleaseSharedQueue, "inherit the existing release; do not create another one")
	require.Equal(t, f.c.Config.SourceSHA, plan.RuntimeSHA)
	require.NoError(t, f.m.ActivateSteady(ctx, plan, f.sign(t, plan), "operator"))
	require.NoError(t, f.c.Step(ctx))
	require.Equal(t, root, *f.saved)
	require.Equal(t, checkpoint, f.r.Get(ctx, checkpointKey).Val())
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, queue.digest, after.digest)
	require.Equal(t, queue.fields, after.fields, "original delivery bytes and identity must be retained")
	require.Zero(t, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	require.False(t, f.facts[held].Eligible, "issuing a recovery purpose never releases the hold")
	normal := f.addSource()
	work, _, err := BindApprovedSource(ctx, normal.SourceID, normal.ConfigSHA256)
	require.NoError(t, err)
	require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, normal.SourceID)))
	require.Error(
		t,
		f.m.ActivateSteady(ctx, plan, f.sign(t, plan), "operator"),
		"activation replay stays closed",
	)
}

func TestNormalSharedContinuationRefusesLostOrTamperedEvidence(t *testing.T) {
	for _, scenario := range []string{
		"checkpoint_missing", "checkpoint_changed", "old_signature_changed", "root_revoked",
		"new_checkpoint_substituted",
	} {
		t.Run(scenario, func(t *testing.T) {
			f, held, key := normalSharedContinuationFixture(t)
			ctx := context.Background()
			plan, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1)
			require.NoError(t, err)
			before := f.r.Get(ctx, f.c.prefix+"recovery").Val()
			switch scenario {
			case "checkpoint_missing":
				require.NoError(t, f.r.Del(ctx, key).Err())
			case "checkpoint_changed":
				require.NoError(t, f.r.Set(ctx, key, "tampered", 0).Err())
			case "root_revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
			case "new_checkpoint_substituted":
				plan.SharedCheckpointID = uuid.NewString()
			case "old_signature_changed":
				var state recoveryMode
				require.NoError(t, json.Unmarshal([]byte(before), &state))
				state.Steady.Approval.Signature = strings.Repeat("0", 128)
				raw, marshalErr := json.Marshal(state)
				require.NoError(t, marshalErr)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", raw, 0).Err())
				before = string(raw)
			}
			require.Error(t, f.m.ActivateSteady(ctx, plan, f.sign(t, plan), "operator"))
			require.Equal(t, before, f.r.Get(ctx, f.c.prefix+"recovery").Val())
		})
	}
}
