package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ApprovedSyncQueue carries only newly approved continuation deliveries.
const ApprovedSyncQueue = "approved_sync"

// ApprovedSource pins both the configured resource selection and live local
// authority. Normal connector ACL/revision and publication fences still apply.
type ApprovedSource struct {
	SourceID     string `json:"source_id"`
	ConfigSHA256 string `json:"config_sha256"`
	FactsSHA256  string `json:"facts_sha256"`
}

func (s ApprovedSource) matches(f RecoveryFacts) bool {
	return (f.Eligible || f.RetryEligible) && f.SourceID == s.SourceID && s.ConfigSHA256 == f.ConfigSHA256 &&
		s.FactsSHA256 == f.FactsSHA256 && f.SelectedResourceCount > 0 && f.SelectedResourceCount <= 64
}

// ContinuityPlan is a new, separately approved operational act after canary
// review. It never extends a canary approval or resumes the shared sync queue.
type ContinuityPlan struct {
	Version               int              `json:"version"`
	ApprovalKeyID         string           `json:"approval_key_id"`
	ID                    string           `json:"id"`
	ActorID               string           `json:"actor_id"`
	Namespace             string           `json:"namespace"`
	RuntimeSHA            string           `json:"runtime_sha"`
	WorkerID              string           `json:"worker_id"`
	Incarnation           string           `json:"incarnation"`
	ControllerFingerprint string           `json:"controller_fingerprint"`
	MembershipRevision    string           `json:"membership_revision"`
	RecoveryPlanSHA256    string           `json:"recovery_plan_sha256"`
	PreviousModeSHA256    string           `json:"previous_mode_sha256"`
	HistoricalQueueSHA256 string           `json:"historical_queue_sha256"`
	HistoricalPending     int              `json:"historical_pending"`
	Sources               []ApprovedSource `json:"sources"`
	CreatedMS             int64            `json:"created_ms"`
	ExpiresMS             int64            `json:"expires_ms"`
}

func (m *RecoveryManager) checkApprovedSource(ctx context.Context, source, config string) error {
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return ErrRecoveryRejected
	}
	var state recoveryMode
	if json.Unmarshal([]byte(raw), &state) != nil {
		return ErrRecoveryRejected
	}
	if state.Phase != "approved_sources" && state.Phase != "running" {
		return nil
	}
	entry, ok := state.ApprovedSources[source]
	if state.Phase == "running" {
		entry = ApprovedSource{
			SourceID:     state.SourceID,
			ConfigSHA256: state.ConfigSHA256,
			FactsSHA256:  state.FactsSHA256,
		}
		ok = source == state.SourceID && state.FactsSHA256 != ""
	}
	if !ok || entry.ConfigSHA256 != config {
		return ErrRecoveryRejected
	}
	if identity, outbound := feishusafety.RecoveryOutboundExecution(ctx); outbound {
		execution, claimed := types.SyncExecutionFromContext(ctx)
		if !claimed || execution.DataSourceID != source || execution.SyncLogID != identity.Run ||
			execution.Generation != identity.Generation || execution.Generation < 1 || execution.TenantID == 0 {
			return ErrRecoveryRejected
		}
		if state.Phase == "running" && (identity.Run != state.RunID || identity.Generation != state.Generation) {
			return ErrRecoveryRejected
		}
	}
	f, err := m.facts(ctx, source)
	if err != nil || !entry.matches(f) {
		return ErrRecoveryRejected
	}
	if state.Phase == "running" && (!f.Eligible || f.LiveExecutions > 1 || f.LivePublications > 1) {
		return ErrRecoveryRejected
	}
	return nil
}

// BindApprovedSource reconstructs runtime identity from a live signed policy.
// It confers no DB claim: ProcessSync replaces run/generation with its actual
// SQL execution identity, including on retries and scheduled deliveries.
func BindApprovedSource(ctx context.Context, source, config string) (context.Context, bool, error) {
	m := Recovery()
	if m == nil {
		return ctx, false, nil
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	if errors.Is(err, redis.Nil) {
		return ctx, false, nil
	}
	if err != nil {
		return ctx, false, ErrRecoveryRejected
	}
	var state recoveryMode
	if json.Unmarshal([]byte(raw), &state) != nil {
		return ctx, false, ErrRecoveryRejected
	}
	if state.Phase != "approved_sources" {
		return ctx, false, nil
	}
	if err := m.checkApprovedSource(ctx, source, config); err != nil {
		return ctx, true, err
	}
	ctx = feishusafety.WithRecoveryContext(
		ctx,
		feishusafety.RecoveryContext{Plan: state.ContinuityID, Config: config},
	)
	return ctx, true, nil
}

func (m *RecoveryManager) approvedQueueEmpty(ctx context.Context) error {
	r := m.controller.Client
	for _, name := range []string{"pending", "active"} {
		n, err := r.LLen(ctx, "asynq:{"+ApprovedSyncQueue+"}:"+name).Result()
		if err != nil || n != 0 {
			return ErrRecoveryRejected
		}
	}
	for _, name := range []string{"retry", "scheduled", "archived"} {
		n, err := r.ZCard(ctx, "asynq:{"+ApprovedSyncQueue+"}:"+name).Result()
		if err != nil || n != 0 {
			return ErrRecoveryRejected
		}
	}
	return nil
}

// PlanContinuity snapshots a bounded source policy without changing admission.
func (m *RecoveryManager) PlanContinuity(
	ctx context.Context,
	actor string,
	sources []string,
) (ContinuityPlan, error) {
	var p ContinuityPlan
	if m == nil || actor == "" || len(sources) < 1 || len(sources) > 4 {
		return p, ErrRecoveryRejected
	}
	ids := append([]string(nil), sources...)
	sort.Strings(ids)
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.departed || feishusafety.Default() != c.Guard {
		return p, ErrRecoveryRejected
	}
	if c.ApprovedConsumers == nil {
		return p, ErrRecoveryRejected
	}
	consumers, consumerErr := c.ApprovedConsumers(ctx)
	if consumerErr != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
		return p, ErrRecoveryRejected
	}
	if err := c.checkRecoveryPolicy(ctx); err != nil {
		return p, err
	}
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var s recoveryMode
	if err != nil || json.Unmarshal([]byte(raw), &s) != nil ||
		(s.Phase != "awaiting_review" && s.Phase != "approved_sources") {
		return p, ErrRecoveryRejected
	}
	if n, err := c.Client.Exists(ctx, c.prefix+"trip").Result(); err != nil || n != 0 {
		return p, ErrRecoveryRejected
	}
	if permit, err := c.Client.Get(ctx, c.Guard.PermitKey()).Result(); err != nil ||
		permit != c.Guard.Fingerprint() {
		return p, ErrRecoveryRejected
	}
	q, err := m.queue(ctx)
	if err != nil {
		return p, err
	}
	if err := m.approvedQueueEmpty(ctx); err != nil {
		return p, err
	}
	now, err := c.Client.Time(ctx).Result()
	if err != nil {
		return p, err
	}
	p = ContinuityPlan{
		Version: 1, ApprovalKeyID: m.keyID, ID: uuid.NewString(), ActorID: actor,
		Namespace:  strings.TrimSuffix(strings.TrimPrefix(c.prefix, "feishu:safety:{"), "}:"),
		RuntimeSHA: c.Config.SourceSHA, WorkerID: c.Config.WorkerID, Incarnation: c.incarnation,
		ControllerFingerprint: c.fingerprint, MembershipRevision: s.MembershipRevision,
		RecoveryPlanSHA256: s.PlanSHA256, PreviousModeSHA256: digestBytes(raw),
		HistoricalQueueSHA256: q.digest, HistoricalPending: len(q.ids), CreatedMS: now.UnixMilli(),
		ExpiresMS: now.Add(time.Hour).UnixMilli(),
	}
	for i, id := range ids {
		if _, err := uuid.Parse(id); err != nil || (i > 0 && id == ids[i-1]) {
			return ContinuityPlan{}, ErrRecoveryRejected
		}
		f, err := m.facts(ctx, id)
		a := ApprovedSource{SourceID: id, ConfigSHA256: f.ConfigSHA256, FactsSHA256: f.FactsSHA256}
		if err != nil || !f.Eligible || !a.matches(f) || f.LiveExecutions != 0 || f.LivePublications != 0 {
			return ContinuityPlan{}, ErrRecoveryRejected
		}
		p.Sources = append(p.Sources, a)
	}
	return p, nil
}

func (m *RecoveryManager) verifyContinuity(
	p ContinuityPlan,
	a RecoveryApproval,
	actor string,
) error {
	now := time.Now().UnixMilli()
	hash, err := RecoveryDigest(p)
	if err != nil || p.Version != 1 || p.ApprovalKeyID != m.keyID || p.ActorID != actor || actor == "" ||
		p.CreatedMS > now+5000 || p.ExpiresMS <= now || p.ExpiresMS <= p.CreatedMS ||
		p.ExpiresMS-p.CreatedMS > int64(time.Hour/time.Millisecond) ||
		len(p.Sources) < 1 || len(p.Sources) > 4 || a.Version != 2 || a.Algorithm != "Ed25519" || a.KeyID != m.keyID ||
		a.Operation != "approved_sources" || a.Incarnation != p.Incarnation || a.ActorID != actor ||
		a.PlanSHA256 != hash ||
		a.ExpiresMS <= now ||
		a.ExpiresMS > p.ExpiresMS ||
		!recoveryReference.MatchString(a.Reference) ||
		strings.Contains(a.Reference, "://") {
		return ErrRecoveryRejected
	}
	if b, err := hex.DecodeString(a.EvidenceSHA256); err != nil || len(b) != 32 {
		return ErrRecoveryRejected
	}
	if id, err := uuid.Parse(p.ID); err != nil || id.String() != p.ID {
		return ErrRecoveryRejected
	}
	sig, err := hex.DecodeString(a.Signature)
	if err != nil {
		return ErrRecoveryRejected
	}
	a.Signature = ""
	body, err := recoveryCanonical(a)
	if err != nil || !ed25519.Verify(m.key, body, sig) {
		return ErrRecoveryRejected
	}
	return nil
}

// ApproveSources changes only the continuation policy and its dedicated queue.
// Historical shared-queue bytes, trip history and hold/revoked records survive.
func (m *RecoveryManager) ApproveSources(
	ctx context.Context,
	p ContinuityPlan,
	a RecoveryApproval,
	actor string,
) error {
	if m == nil {
		return ErrRecoveryRejected
	}
	m.operation.Lock()
	defer m.operation.Unlock()
	if err := m.verifyContinuity(p, a, actor); err != nil {
		return err
	}
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.RuntimeSHA != c.Config.SourceSHA || p.WorkerID != c.Config.WorkerID ||
		p.Incarnation != c.incarnation ||
		p.ControllerFingerprint != c.fingerprint ||
		"feishu:safety:{"+p.Namespace+"}:" != c.prefix ||
		c.departed ||
		feishusafety.Default() != c.Guard {
		return ErrRecoveryRejected
	}
	entries := map[string]ApprovedSource{}
	if c.ApprovedConsumers == nil {
		return ErrRecoveryRejected
	}
	consumers, err := c.ApprovedConsumers(ctx)
	if err != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
		return ErrRecoveryRejected
	}
	for _, entry := range p.Sources {
		if _, ok := entries[entry.SourceID]; ok {
			return ErrRecoveryRejected
		}
		f, err := m.facts(ctx, entry.SourceID)
		if err != nil || !f.Eligible || !entry.matches(f) || f.LiveExecutions != 0 || f.LivePublications != 0 {
			return ErrRecoveryRejected
		}
		entries[entry.SourceID] = entry
	}
	q, err := m.queue(ctx)
	if err != nil || q.digest != p.HistoricalQueueSHA256 || len(q.ids) != p.HistoricalPending {
		return ErrRecoveryRejected
	}
	keys := []string{
		c.prefix + "trip",
		c.prefix + "recovery",
		c.prefix + "recovery_required",
		c.Guard.PermitKey(),
		c.prefix + "control_audit",
		c.prefix + "workers",
		c.prefix + "admitted",
		c.prefix + "membership_revision",
		c.prefix + "leases",
		"asynq:{sync}:paused",
	}
	for _, queue := range []string{"sync", ApprovedSyncQueue} {
		for _, name := range []string{"pending", "active", "retry", "scheduled", "archived", "paused"} {
			keys = append(keys, "asynq:{"+queue+"}:"+name)
		}
	}
	for _, id := range q.ids {
		keys = append(keys, "asynq:{sync}:t:"+id)
	}
	return c.Client.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, c.prefix+"recovery").Result()
		var state recoveryMode
		if err != nil || digestBytes(raw) != p.PreviousModeSHA256 ||
			json.Unmarshal([]byte(raw), &state) != nil ||
			(state.Phase != "awaiting_review" && state.Phase != "approved_sources") ||
			state.PlanSHA256 != p.RecoveryPlanSHA256 {
			return ErrRecoveryRejected
		}
		if err := c.checkRecoveryPolicy(ctx); err != nil {
			return err
		}
		if n, err := tx.Exists(ctx, c.prefix+"trip").Result(); err != nil || n != 0 {
			return ErrRecoveryRejected
		}
		if permit, err := tx.Get(ctx, c.Guard.PermitKey()).Result(); err != nil ||
			permit != c.Guard.Fingerprint() {
			return ErrRecoveryRejected
		}
		if ttl, err := tx.PTTL(ctx, c.Guard.PermitKey()).Result(); err != nil || ttl <= 0 {
			return ErrRecoveryRejected
		}
		if revision, err := tx.Get(ctx, c.prefix+"membership_revision").Result(); err != nil ||
			revision != p.MembershipRevision {
			return ErrRecoveryRejected
		}
		identity := c.fingerprint + ":" + c.incarnation
		for _, name := range []string{"workers", "admitted"} {
			value, err := tx.HGet(ctx, c.prefix+name, c.Config.WorkerID).Result()
			if err != nil || value != identity {
				return ErrRecoveryRejected
			}
		}
		now, err := tx.Time(ctx).Result()
		if err != nil || state.ExpiresMS <= now.UnixMilli() || a.ExpiresMS <= now.UnixMilli() {
			return ErrRecoveryRejected
		}
		lease, err := tx.ZScore(ctx, c.prefix+"leases", c.Config.WorkerID).Result()
		if err != nil || lease <= float64(now.UnixMilli()) {
			return ErrRecoveryRejected
		}
		current, err := m.queue(ctx)
		if err != nil || current.digest != p.HistoricalQueueSHA256 ||
			len(current.ids) != p.HistoricalPending {
			return ErrRecoveryRejected
		}
		if err := m.approvedQueueEmpty(ctx); err != nil {
			return err
		}
		state.Phase = "approved_sources"
		state.ContinuityID = p.ID
		state.ApprovedSources = entries
		state.HistoricalQueueSHA256 = p.HistoricalQueueSHA256
		state.HistoricalPending = p.HistoricalPending
		state.ExpiresMS = a.ExpiresMS
		state.RunID = ""
		state.Generation = 0
		body, err := json.Marshal(state)
		if err != nil {
			return err
		}
		signed, _ := json.Marshal(a)
		plan, _ := json.Marshal(p)
		hash, _ := RecoveryDigest(p)
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			// One Lua command keeps audit failure ahead of all state changes;
			// separate MULTI commands would not roll back after an XADD error.
			pipe.Eval(
				ctx,
				`
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[2])~=0 or
 redis.call('GET',KEYS[3])~=ARGV[2] or redis.call('PTTL',KEYS[3])<=0 then
 return redis.error_reply('CONTINUITY_CHANGED') end
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if tonumber(ARGV[3])<=now then return redis.error_reply('CONTINUITY_EXPIRED') end
redis.call('XADD',KEYS[4],'*','event','approved_sources_activated','plan_sha256',ARGV[4],
 'actor_id',ARGV[5],'signed_approval',ARGV[6],'plan',ARGV[7],'historical_queue_sha256',ARGV[8])
redis.call('SET',KEYS[1],ARGV[9]);redis.call('DEL',KEYS[5]);return 1`,
				[]string{
					c.prefix + "recovery",
					c.prefix + "trip",
					c.Guard.PermitKey(),
					c.prefix + "control_audit",
					"asynq:{" + ApprovedSyncQueue + "}:paused",
				},
				raw,
				c.Guard.Fingerprint(),
				a.ExpiresMS,
				hash,
				actor,
				string(signed),
				string(plan),
				p.HistoricalQueueSHA256,
				string(body),
			)
			return nil
		})
		return err
	}, keys...)
}
