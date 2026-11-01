package feishusynccontrol

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

type (
	reconciliationContextKey struct{}
	reconciliationIdentity   struct{ Policy, Source string }
)

const reconciliationTicketFence = `
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('GET',KEYS[6])~=ARGV[4] or redis.call('PTTL',KEYS[6])<=0 then
 return redis.error_reply('RECONCILIATION_HEALTH_LOST') end
local s=cjson.decode(ARGV[1]);local allowed=false
if s.phase=='approved_sources' and s.continuity_id==ARGV[4] and s.steady_policy then
 for _,entry in ipairs(s.steady_policy.plan.managed_sources) do
  if entry.source_id==ARGV[3] and entry.reconciliation==true and
   entry.reconcile_until_ms and entry.reconcile_until_ms>now then allowed=true end
 end
end
if not allowed then return redis.error_reply('RECONCILIATION_PURPOSE_EXPIRED') end
`

// SourceEligibility reports local metadata after live source management checks;
// it is not proof that a remote resource can currently be read or published.
func (m *RecoveryManager) SourceEligibility(
	ctx context.Context, source, actor string,
) (SourceManagementAuthority, RecoveryFacts, error) {
	if m == nil || m.steadyAuthority == nil {
		return SourceManagementAuthority{}, RecoveryFacts{}, ErrRecoveryRejected
	}
	authority, err := m.steadyAuthority(ctx, source, actor)
	if err != nil {
		return SourceManagementAuthority{}, RecoveryFacts{}, err
	}
	facts, err := m.facts(ctx, source)
	return authority, facts, err
}

func reconciliationContextMatches(ctx context.Context, policy, source string) bool {
	v, ok := ctx.Value(reconciliationContextKey{}).(reconciliationIdentity)
	return ok && v.Policy == policy && v.Source == source
}

// ReconciliationPolicy returns server-issued execution purpose, never a caller
// parameter or a source-state override. Source access remains closed until the
// existing full-reconciliation publication transaction succeeds.
func ReconciliationPolicy(ctx context.Context, source string) string {
	v, ok := ctx.Value(reconciliationContextKey{}).(reconciliationIdentity)
	if !ok || v.Source != source {
		return ""
	}
	return v.Policy
}

func (m *RecoveryManager) reconciliationContext(
	ctx context.Context, source, policy string, worker bool,
) (context.Context, string, error) {
	if m == nil {
		return ctx, "", ErrRecoveryRejected
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	var state recoveryMode
	if err != nil || json.Unmarshal([]byte(raw), &state) != nil || state.Steady == nil ||
		state.ContinuityID != policy {
		return ctx, "", ErrRecoveryRejected
	}
	var selected *SteadySource
	for i := range state.Steady.Plan.ManagedSources {
		s := &state.Steady.Plan.ManagedSources[i]
		if s.SourceID == source {
			selected = s
			break
		}
	}
	if selected == nil || !selected.Reconciliation {
		return ctx, "", ErrRecoveryRejected
	}
	if worker {
		e, ok := types.SyncExecutionFromContext(ctx)
		if !ok || e.DataSourceID != source || e.Generation < 1 || e.TenantID != selected.Authority.TenantID {
			return ctx, "", ErrRecoveryRejected
		}
		bound, err := m.controller.Client.HGet(ctx, m.reconciliationKey(policy, source), "run_id").Result()
		if err != nil || bound != e.SyncLogID {
			return ctx, "", ErrRecoveryRejected
		}
	} else if types.PrivateKBReadUser(ctx) != state.Steady.Plan.ActorID {
		return ctx, "", ErrRecoveryRejected
	}
	ctx = context.WithValue(ctx, reconciliationContextKey{}, reconciliationIdentity{Policy: policy, Source: source})
	if err := m.checkApprovedSource(ctx, source, selected.ConfigSHA256); err != nil {
		return ctx, "", err
	}
	if err := m.steadyReady(ctx); err != nil {
		return ctx, "", err
	}
	return ctx, raw, nil
}

func (m *RecoveryManager) reconciliationKey(policy, source string) string {
	return m.controller.prefix + "source_reconciliation:" + policy + ":" + source
}

// BeginSourceReconciliation consumes one independently signed source purpose.
// The ticket and audit are persistent. An uncertain enqueue is not permission
// to create another execution: a new signed policy is required to try again.
func (m *RecoveryManager) BeginSourceReconciliation(
	ctx context.Context, source, policy string,
) (context.Context, error) {
	work, raw, err := m.reconciliationContext(ctx, source, policy, false)
	if err != nil {
		return ctx, err
	}
	err = m.controller.Client.Eval(ctx, reconciliationTicketFence+`
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[2])~=0 or
 redis.call('GET',KEYS[3])~=ARGV[2] or redis.call('PTTL',KEYS[3])<=0 or
 redis.call('EXISTS',KEYS[4])~=0 then return redis.error_reply('RECONCILIATION_DENIED') end
redis.call('XADD',KEYS[5],'*','event','source_reconciliation_reserved','source_id',ARGV[3],
 'policy_id',ARGV[4],'actor_id',ARGV[5])
redis.call('HSET',KEYS[4],'source_id',ARGV[3],'policy_id',ARGV[4],'actor_id',ARGV[5],'run_id','')
return 1`, []string{
		m.controller.prefix + "recovery", m.controller.prefix + "trip",
		m.controller.Guard.PermitKey(), m.reconciliationKey(policy, source), m.controller.prefix + "control_audit",
		m.controller.prefix + "steady_health",
	},
		raw, m.controller.Guard.Fingerprint(), source, policy, types.PrivateKBReadUser(ctx)).Err()
	return work, err
}

// BindSourceReconciliationRun binds the single ticket to the normal SQL-created
// SyncLog before delivery. It does not claim, change state or release quarantine.
func (m *RecoveryManager) BindSourceReconciliationRun(ctx context.Context, source, run string) error {
	policy := ReconciliationPolicy(ctx, source)
	if _, err := uuid.Parse(run); policy == "" || err != nil {
		return ErrRecoveryRejected
	}
	_, raw, err := m.reconciliationContext(ctx, source, policy, false)
	if err != nil {
		return err
	}
	return m.controller.Client.Eval(ctx, reconciliationTicketFence+`
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[2])~=0 or
 redis.call('GET',KEYS[3])~=ARGV[2] or redis.call('PTTL',KEYS[3])<=0 or
 redis.call('HGET',KEYS[4],'run_id')~='' then return redis.error_reply('RECONCILIATION_CHANGED') end
redis.call('XADD',KEYS[5],'*','event','source_reconciliation_run_bound','source_id',ARGV[3],
 'policy_id',ARGV[4],'run_id',ARGV[5])
redis.call('HSET',KEYS[4],'run_id',ARGV[5]);return 1`,
		[]string{
			m.controller.prefix + "recovery", m.controller.prefix + "trip", m.controller.Guard.PermitKey(),
			m.reconciliationKey(policy, source), m.controller.prefix + "control_audit",
			m.controller.prefix + "steady_health",
		},
		raw, m.controller.Guard.Fingerprint(), source, policy, run).Err()
}

// RestoreSourceReconciliation requires the real claimed SQL execution and its
// previously bound ticket. Merely adding a policy ID to a task grants nothing.
func RestoreSourceReconciliation(ctx context.Context, source, policy string) (context.Context, error) {
	m := Recovery()
	if m == nil {
		return ctx, ErrRecoveryRejected
	}
	work, _, err := m.reconciliationContext(ctx, source, policy, true)
	if err == nil {
		work = types.WithLegacyReconciliationRead(work)
	}
	return work, err
}

// Keep the one-shot read window independent from the normal-service grant.
const sourceReconciliationWindow = time.Hour
