package feishusynccontrol

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
)

func validFrozenRecovery(s recoveryMode, source string) bool {
	return s.Version == 1 && s.Phase == "frozen" && s.QueueName == "approved_sync" &&
		!s.QueueReleased && s.RunID == "" && s.Steady == nil && s.ContinuityID == "" &&
		len(s.ApprovedSources) == 0 && s.PlanID != "" && len(s.PlanSHA256) == 64 && s.SourceID == source
}

// The caller holds controller.mu. This is a fresh approval plan, not an
// extension of an expired approval and never a reason to resume a shared queue.
func (m *RecoveryManager) planFrozenRecovery(
	ctx context.Context, source, actor string, facts RecoveryFacts, q recoveryQueueSnapshot,
) (RecoveryPlan, error) {
	c := m.controller
	var p RecoveryPlan
	if q.name != "approved_sync" || actor == "" || c.departed || feishusafety.Default() != c.Guard {
		return p, ErrRecoveryRejected
	}
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var previous recoveryMode
	if err != nil || json.Unmarshal([]byte(raw), &previous) != nil || !validFrozenRecovery(previous, source) ||
		previous.ConfigSHA256 != facts.ConfigSHA256 {
		return p, ErrRecoveryRejected
	}
	required, err := c.Client.Get(ctx, c.prefix+"recovery_required").Result()
	if err != nil || required != previous.PlanSHA256 {
		return p, ErrRecoveryRejected
	}
	workers, err := c.Client.HGetAll(ctx, c.prefix+"workers").Result()
	if err != nil || len(workers) != 1 || workers[c.Config.WorkerID] != c.fingerprint+":"+c.incarnation {
		return p, ErrRecoveryRejected
	}
	revision, err := c.Client.Get(ctx, c.prefix+"membership_revision").Result()
	if err != nil {
		return p, err
	}
	permit, err := c.Client.Get(ctx, c.Guard.PermitKey()).Result()
	if err != nil || permit != c.Guard.Fingerprint() {
		return p, ErrRecoveryRejected
	}
	now := time.Now().UnixMilli()
	p = RecoveryPlan{
		ResetMode: "refresh_frozen", ApprovalKeyID: m.keyID, Version: 1, PlanID: uuid.NewString(), ActorID: actor,
		Namespace: strings.TrimSuffix(strings.TrimPrefix(c.prefix, "feishu:safety:{"), "}:"),
		AppSHA:    c.Config.SourceSHA, Trip: "frozen_recovery", IncidentID: previous.PlanID,
		WorkerID: c.Config.WorkerID, Incarnation: c.incarnation, ControllerFingerprint: c.fingerprint,
		MembershipRevision: revision, QueueSHA256: q.digest, QueuePendingCount: len(q.ids), QueueName: q.name,
		PreviousModeSHA256: digestBytes(raw), Facts: facts, CreatedMS: now,
		ExpiresMS: now + int64((15*time.Minute)/time.Millisecond),
	}
	return p, nil
}

// refreshFrozenRecovery updates only the approved recovery envelope. The queue
// stays paused, every binary task and checkpoint is retained, and canary and
// observation approvals remain mandatory before normal service can resume it.
func (m *RecoveryManager) refreshFrozenRecovery(
	ctx context.Context, p RecoveryPlan, approval RecoveryApproval, actor string,
) error {
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.AppSHA != c.Config.SourceSHA || p.WorkerID != c.Config.WorkerID || p.Incarnation != c.incarnation ||
		p.ControllerFingerprint != c.fingerprint || "feishu:safety:{"+p.Namespace+"}:" != c.prefix ||
		c.departed || feishusafety.Default() != c.Guard || p.Trip != "frozen_recovery" {
		return ErrRecoveryRejected
	}
	facts, err := m.facts(ctx, p.Facts.SourceID)
	if err != nil || !facts.Eligible || !sameRecoveryFacts(facts, p.Facts) {
		return ErrRecoveryRejected
	}
	q, err := m.incidentQueue(ctx)
	if err != nil || q.name != "approved_sync" || !recoveryQueueMatches(q, p) {
		return ErrRecoveryRejected
	}
	previous, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var old recoveryMode
	if err != nil || digestBytes(previous) != p.PreviousModeSHA256 ||
		json.Unmarshal([]byte(previous), &old) != nil || !validFrozenRecovery(old, facts.SourceID) ||
		old.PlanID != p.IncidentID || old.ConfigSHA256 != facts.ConfigSHA256 {
		return ErrRecoveryRejected
	}
	if c.Consumers == nil {
		return ErrRecoveryRejected
	}
	consumers, err := c.Consumers(ctx)
	if err != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
		return ErrRecoveryRejected
	}
	normal, err := c.Client.Get(ctx, c.prefix+"normal_service").Result()
	if err != nil || normal == "" {
		return ErrRecoveryRejected
	}
	hash, err := RecoveryDigest(p)
	if err != nil {
		return err
	}
	state, _ := json.Marshal(recoveryMode{
		QueueName: q.name, Version: 1, RuntimeSHA: p.AppSHA, WorkerID: p.WorkerID, Incarnation: p.Incarnation,
		ControllerFingerprint: p.ControllerFingerprint, MembershipRevision: p.MembershipRevision,
		PlanID: p.PlanID, PlanSHA256: hash, SourceID: facts.SourceID, ConfigSHA256: facts.ConfigSHA256,
		FactsSHA256: facts.FactsSHA256, Phase: "frozen", ExpiresMS: p.ExpiresMS,
	})
	ids, _ := json.Marshal(q.ids)
	args := []any{
		p.Trip, p.IncidentID, p.MembershipRevision, c.fingerprint + ":" + c.incarnation,
		string(state), previous, normal, string(ids),
	}
	for _, fields := range q.fields {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		args = append(args, len(keys))
		for _, key := range keys {
			args = append(args, key, []byte(fields[key]))
		}
	}
	signed, _ := json.Marshal(approval)
	args = append(args, q.pause, string(signed), p.WorkerID, hash, c.Guard.Fingerprint(), actor)
	err = recoveryIncidentScript(q, `
local old=cjson.decode(ARGV[6]);local state=cjson.decode(ARGV[5])
local now=redis.call('TIME');local ms=now[1]*1000+math.floor(now[2]/1000)
if redis.call('EXISTS',KEYS[1])~=0 or redis.call('GET',KEYS[2])~=ARGV[6] or
 redis.call('GET',KEYS[3])~=old.plan_sha256 or redis.call('GET',KEYS[4])~=ARGV[7] or
 redis.call('GET',KEYS[5])~=ARGV[3] or redis.call('HLEN',KEYS[6])~=1 or
 redis.call('HGET',KEYS[6],ARGV[#ARGV-3])~=ARGV[4] or redis.call('HLEN',KEYS[8])~=0 or
 redis.call('HLEN',KEYS[9])~=1 or redis.call('HGET',KEYS[9],ARGV[#ARGV-3])~=ARGV[4] or
 redis.call('ZCARD',KEYS[7])~=1 or tonumber(redis.call('ZSCORE',KEYS[7],ARGV[#ARGV-3]) or '0')<=ms or
 redis.call('GET',KEYS[10])~=ARGV[#ARGV-1] or redis.call('PTTL',KEYS[10])<=0 or
 redis.call('GET','asynq:{sync}:paused')~=ARGV[#ARGV-5] or state.expires_ms<=ms or
 old.phase~='frozen' or old.queue_name~='approved_sync' or old.plan_id~=ARGV[2] then return
 redis.error_reply('RECOVERY_FROZEN_CHANGED') end
redis.call('SET',KEYS[2],ARGV[5]);redis.call('SET',KEYS[3],ARGV[#ARGV-2])
redis.call('XADD',KEYS[11],'*','event','recovery_frozen_refresh','previous_plan_id',old.plan_id,
 'plan_sha256',ARGV[#ARGV-2],'actor_id',ARGV[#ARGV],'signed_approval',ARGV[#ARGV-4])
return 1`).Eval(ctx, c.Client, []string{
		c.prefix + "trip", c.prefix + "recovery", c.prefix + "recovery_required", c.prefix + "normal_service",
		c.prefix + "membership_revision", c.prefix + "workers", c.prefix + "leases", c.prefix + "inflight",
		c.prefix + "admitted", c.Guard.PermitKey(), c.prefix + "control_audit",
	}, args...).Err()
	if err != nil {
		return ErrRecoveryRejected
	}
	return nil
}
