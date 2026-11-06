package feishusynccontrol

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func normalContinuationFixture(t *testing.T, staleWorker bool) (*normalFixture, string) {
	t.Helper()
	f := newNormalFixture(t)
	f.activateNormal(t)
	held := f.ids[0]
	facts := f.facts[held]
	facts.Eligible, facts.ReconciliationEligible = false, true
	f.facts[held] = facts
	authority := f.authority[held]
	authority.SourceStatus, authority.AccessState = "error", "hold"
	f.authority[held] = authority
	ctx := context.Background()
	raw, err := f.r.Get(ctx, f.c.prefix+"recovery").Result()
	require.NoError(t, err)
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(raw), &state))
	state.ExpiresMS = time.Now().Add(-time.Minute).UnixMilli()
	if staleWorker {
		state.RuntimeSHA = strings.Repeat("0", 40)
		state.WorkerID = "departed-worker"
		state.Incarnation = uuid.NewString()
		state.MembershipRevision = "retired-membership"
	}
	body, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", body, 0).Err())
	return f, held
}

type normalParentRaceHook struct {
	once   sync.Once
	change func()
}

func (h *normalParentRaceHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *normalParentRaceHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *normalParentRaceHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "watch" {
			h.once.Do(h.change)
		}
		return next(ctx, cmd)
	}
}

func TestNormalContinuationRejectsParentChangeBeforeWatchBegins(t *testing.T) {
	f, held := normalContinuationFixture(t, true)
	ctx := context.Background()
	plan, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1)
	require.NoError(t, err)
	before := f.r.Get(ctx, f.c.prefix+"recovery").Val()
	writer := redis.NewClient(f.r.Options())
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	fired := false
	f.r.AddHook(&normalParentRaceHook{change: func() {
		fired = true
		revoked := *f.saved
		revoked.Active = false
		raw, err := json.Marshal(revoked)
		require.NoError(t, err)
		require.NoError(t, writer.Set(ctx, f.c.prefix+"normal_service", raw, 0).Err())
	}})
	require.Error(t, f.m.ActivateSteady(ctx, plan, f.sign(t, plan), "operator"))
	require.True(t, fired)
	require.Equal(t, before, f.r.Get(ctx, f.c.prefix+"recovery").Val(),
		"the final atomic root comparison must close the pre-WATCH revocation window")
}

func TestNormalContinuationSignsFreshRecoveryWithoutStoppingNormalService(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired_review", true: "replaced_worker"}[stale], func(t *testing.T) {
			f, held := normalContinuationFixture(t, stale)
			ctx := context.Background()
			before := *f.saved
			queueBefore, err := f.m.queue(ctx)
			require.NoError(t, err)
			normal := f.addSource()
			_, _, err = BindApprovedSource(ctx, normal.SourceID, normal.ConfigSHA256)
			require.NoError(t, err)
			_, _, err = BindApprovedSource(ctx, held, f.facts[held].ConfigSHA256)
			require.Error(t, err, "a normal root cannot directly open a hold")
			plan, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1, 3*time.Hour)
			require.NoError(t, err)
			require.Equal(t, 2, plan.Version)
			require.Equal(t, before.Authorization.ID, plan.NormalServiceAuthorizationID)
			require.Equal(t, f.c.Config.SourceSHA, plan.RuntimeSHA)
			require.Equal(t, f.c.Config.WorkerID, plan.WorkerID)
			require.Equal(t, f.r.Get(ctx, f.c.prefix+"membership_revision").Val(), plan.MembershipRevision)
			require.False(t, plan.ReleaseSharedQueue)
			approval := f.sign(t, plan)
			require.NoError(t, f.m.ActivateSteady(ctx, plan, approval, "operator"))
			require.Error(t, f.m.ActivateSteady(ctx, plan, approval, "operator"), "old activation cannot be replayed")
			require.NoError(t, f.c.Step(ctx))
			require.Equal(t, before, *f.saved, "do not replace or extend the normal root")
			require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
			queueAfter, err := f.m.queue(ctx)
			require.NoError(t, err)
			require.Equal(t, queueBefore.digest, queueAfter.digest)
			require.Equal(t, queueBefore.fields, queueAfter.fields)
			work, _, err := BindApprovedSource(ctx, normal.SourceID, normal.ConfigSHA256)
			require.NoError(t, err)
			require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, normal.SourceID)))
			_, _, err = BindApprovedSource(ctx, held, f.facts[held].ConfigSHA256)
			require.Error(t, err, "independent reconciliation context and actual SQL claim remain required")
		})
	}
}

func TestNormalContinuationRejectsUntrustedParentOrChangedAuthority(t *testing.T) {
	for _, scenario := range []string{"no_root", "tampered_root", "unknown_mode", "ordinary_scope", "revoked_source"} {
		t.Run(scenario, func(t *testing.T) {
			f, held := normalContinuationFixture(t, true)
			ctx := context.Background()
			var sources []string
			reconcile := []string{held}
			switch scenario {
			case "no_root":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
			case "tampered_root":
				record := *f.saved
				record.Authorization.SourcePolicy = "untrusted"
				raw, err := json.Marshal(record)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", raw, 0).Err())
			case "unknown_mode":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", `{"phase":"running"}`, 0).Err())
			case "ordinary_scope":
				sources, reconcile = []string{f.ids[1]}, nil
			case "revoked_source":
				facts := f.facts[held]
				facts.ReconciliationEligible = false
				f.facts[held] = facts
			}
			_, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", sources, reconcile, 1)
			require.Error(t, err)
		})
	}
	for _, scenario := range []string{"root_revoked", "root_id_changed", "source_authority_changed", "signature_changed"} {
		t.Run(scenario, func(t *testing.T) {
			f, held := normalContinuationFixture(t, true)
			ctx := context.Background()
			plan, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1)
			require.NoError(t, err)
			switch scenario {
			case "root_revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
			case "root_id_changed":
				plan.NormalServiceAuthorizationID = uuid.NewString()
			case "source_authority_changed":
				authority := f.authority[held]
				authority.AuthorityHash = strings.Repeat("e", 64)
				f.authority[held] = authority
			}
			approval := f.sign(t, plan)
			if scenario == "signature_changed" {
				approval.Signature = strings.Repeat("0", 128)
			}
			require.Error(t, f.m.ActivateSteady(ctx, plan, approval, "operator"))
		})
	}
}
