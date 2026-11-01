package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	steadyHealthTTL  = 15 * time.Second
	maxSteadySources = 64
)

var recoverySHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

// SteadySource binds a specific resource selection to its live managing actor.
type SteadySource struct {
	ApprovedSource
	Authority SourceManagementAuthority `json:"authority"`
}

// SteadyPlan separates a short signature-consumption window from an explicitly
// approved normal-service authorization period. Health leases never extend it.
type SteadyPlan struct {
	ContinuityPlan
	AuthorizationUntilMS int64          `json:"authorization_until_ms"`
	PolicyRevision       int64          `json:"policy_revision"`
	RevocationOnly       bool           `json:"revocation_only"`
	ManagedSources       []SteadySource `json:"managed_sources"`
}

// SteadyCertificate retains the exact signed policy for every subsequent check.
type SteadyCertificate struct {
	Plan        SteadyPlan       `json:"plan"`
	Approval    RecoveryApproval `json:"approval"`
	ActivatedMS int64            `json:"activated_ms"`
}

// ConfigureSteadyAuthority installs the server-owned live authorization loader.
func (m *RecoveryManager) ConfigureSteadyAuthority(
	load func(context.Context, string, string) (SourceManagementAuthority, error),
) {
	m.steadyAuthority = load
	m.controller.steadyManager = m
}

func (m *RecoveryManager) steadySource(ctx context.Context, source, actor string) (SteadySource, RecoveryFacts, error) {
	var s SteadySource
	if m.steadyAuthority == nil {
		return s, RecoveryFacts{}, ErrRecoveryRejected
	}
	authority, err := m.steadyAuthority(ctx, source, actor)
	if err != nil || authority.SourceID != source || authority.ActorID != actor || authority.TenantID == 0 ||
		authority.KBID == "" || !recoverySHA.MatchString(authority.AuthorityHash) {
		return s, RecoveryFacts{}, ErrRecoveryRejected
	}
	f, err := m.facts(ctx, source)
	s = SteadySource{ApprovedSource: ApprovedSource{
		SourceID: source, ConfigSHA256: f.ConfigSHA256,
		FactsSHA256: f.FactsSHA256,
	}, Authority: authority}
	if err != nil || !s.matches(f) {
		return s, f, ErrRecoveryRejected
	}
	return s, f, nil
}

func sameSteadySource(current, approved SteadySource, facts RecoveryFacts) bool {
	if approved.Authority.SourceStatus != "active" {
		return false
	}
	if current.Authority.SourceStatus == "error" && facts.RetryEligible {
		current.Authority.SourceStatus = "active"
	}
	return current == approved
}

func (m *RecoveryManager) steadyReady(ctx context.Context) error {
	c := m.controller
	if c.departed || feishusafety.Default() != c.Guard || c.ApprovedConsumers == nil {
		return ErrRecoveryRejected
	}
	consumers, err := c.ApprovedConsumers(ctx)
	if err != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
		return ErrRecoveryRejected
	}
	trip, err := c.Client.Exists(ctx, c.prefix+"trip").Result()
	if err != nil || trip != 0 {
		return ErrRecoveryRejected
	}
	permit, err := c.Client.Get(ctx, c.Guard.PermitKey()).Result()
	if err != nil || permit != c.Guard.Fingerprint() {
		return ErrRecoveryRejected
	}
	ttl, err := c.Client.PTTL(ctx, c.Guard.PermitKey()).Result()
	if err != nil || ttl <= 0 {
		return ErrRecoveryRejected
	}
	return nil
}

// PlanSteady authorizes every selected space independently; an anchor-source
// administrator cannot inherit other spaces or another user's private KB.
func (m *RecoveryManager) PlanSteady(ctx context.Context, actor string, ids []string, days int) (SteadyPlan, error) {
	var p SteadyPlan
	if m == nil || actor == "" || len(ids) > maxSteadySources || days < 1 || days > 30 {
		return p, ErrRecoveryRejected
	}
	if !m.operation.TryLock() {
		return p, ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	// Keep database I/O outside the control mutex: the healthy controller must
	// continue renewing its short permit while multiple tenants are checked.
	type checkedSource struct {
		entry SteadySource
		facts RecoveryFacts
	}
	checked := map[string]checkedSource{}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return p, ErrRecoveryRejected
		}
		if _, duplicate := checked[id]; duplicate {
			return p, ErrRecoveryRejected
		}
		entry, facts, err := m.steadySource(ctx, id, actor)
		if err != nil {
			return p, ErrRecoveryRejected
		}
		checked[id] = checkedSource{entry: entry, facts: facts}
	}
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := m.steadyReady(ctx); err != nil {
		return p, err
	}
	if err := c.checkRecoveryPolicy(ctx); err != nil {
		return p, err
	}
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var s recoveryMode
	if err != nil || json.Unmarshal([]byte(raw), &s) != nil ||
		(s.Phase != "awaiting_review" && s.Phase != "approved_sources") ||
		(len(ids) == 0 && s.Steady == nil) || c.departed || feishusafety.Default() != c.Guard {
		return p, ErrRecoveryRejected
	}
	if s.Steady != nil && s.Steady.Plan.ActorID != actor {
		return p, ErrRecoveryRejected
	}
	q, err := m.queue(ctx)
	if err != nil {
		return p, err
	}
	// First admission and replacements wait for a quiescent dedicated lane.
	// No historical shared-queue task is removed, recreated or acknowledged.
	revokeOnly := s.Steady != nil && len(ids) < len(s.ApprovedSources)
	for _, id := range ids {
		if _, ok := s.ApprovedSources[id]; !ok {
			revokeOnly = false
		}
	}
	if !revokeOnly {
		if err := m.approvedQueueEmpty(ctx); err != nil {
			return p, err
		}
	}
	now, err := c.Client.Time(ctx).Result()
	if err != nil {
		return p, err
	}
	p.ContinuityPlan = ContinuityPlan{
		Version: 1, ApprovalKeyID: m.keyID, ID: uuid.NewString(), ActorID: actor,
		Namespace:  strings.TrimSuffix(strings.TrimPrefix(c.prefix, "feishu:safety:{"), "}:"),
		RuntimeSHA: c.Config.SourceSHA, WorkerID: c.Config.WorkerID, Incarnation: c.incarnation,
		ControllerFingerprint: c.fingerprint, MembershipRevision: s.MembershipRevision,
		RecoveryPlanSHA256: s.PlanSHA256, PreviousModeSHA256: digestBytes(raw),
		HistoricalQueueSHA256: q.digest, HistoricalPending: len(q.ids),
		CreatedMS: now.UnixMilli(), ExpiresMS: now.Add(15 * time.Minute).UnixMilli(),
		Sources: []ApprovedSource{},
	}
	p.PolicyRevision = 1
	if s.Steady != nil {
		p.PolicyRevision = s.Steady.Plan.PolicyRevision + 1
	}
	p.AuthorizationUntilMS = now.Add(time.Duration(days) * 24 * time.Hour).UnixMilli()
	p.RevocationOnly = revokeOnly
	if revokeOnly {
		p.AuthorizationUntilMS = s.Steady.Plan.AuthorizationUntilMS
		if p.ExpiresMS >= p.AuthorizationUntilMS {
			p.ExpiresMS = p.AuthorizationUntilMS - 1
		}
	}
	p.ManagedSources = []SteadySource{}
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for i, id := range ordered {
		if _, err := uuid.Parse(id); err != nil || (i > 0 && id == ordered[i-1]) {
			return SteadyPlan{}, ErrRecoveryRejected
		}
		entry, facts := checked[id].entry, checked[id].facts
		if revokeOnly {
			var previous *SteadySource
			for i := range s.Steady.Plan.ManagedSources {
				if s.Steady.Plan.ManagedSources[i].SourceID == id {
					previous = &s.Steady.Plan.ManagedSources[i]
					break
				}
			}
			if previous == nil || !sameSteadySource(entry, *previous, facts) {
				return SteadyPlan{}, ErrRecoveryRejected
			}
			entry = *previous
		} else if !facts.Eligible || entry.Authority.SourceStatus != "active" ||
			facts.LiveExecutions != 0 || facts.LivePublications != 0 {
			return SteadyPlan{}, ErrRecoveryRejected
		}
		p.ManagedSources = append(p.ManagedSources, entry)
		p.Sources = append(p.Sources, entry.ApprovedSource)
	}
	return p, nil
}

func (m *RecoveryManager) verifySteady(cert SteadyCertificate, now int64, consume bool) error {
	p, a := cert.Plan, cert.Approval
	hash, err := RecoveryDigest(p)
	if err != nil || p.Version != 1 || p.PolicyRevision < 1 || p.ApprovalKeyID != m.keyID ||
		len(p.ManagedSources) > maxSteadySources || len(p.ManagedSources) != len(p.Sources) ||
		p.ActorID == "" || p.CreatedMS > now+5000 || p.ExpiresMS <= p.CreatedMS ||
		p.ExpiresMS-p.CreatedMS > int64(15*time.Minute/time.Millisecond) ||
		p.AuthorizationUntilMS <= now || p.AuthorizationUntilMS <= p.ExpiresMS ||
		p.AuthorizationUntilMS-p.CreatedMS > int64(30*24*time.Hour/time.Millisecond) ||
		a.Version != 2 || a.Algorithm != "Ed25519" || a.KeyID != m.keyID || a.Operation != "steady_sources" ||
		a.PlanSHA256 != hash || a.ActorID != p.ActorID || a.Incarnation != p.Incarnation ||
		a.ExpiresMS > p.ExpiresMS || a.ExpiresMS <= p.CreatedMS || !recoverySHA.MatchString(a.EvidenceSHA256) ||
		!recoveryReference.MatchString(a.Reference) || strings.Contains(a.Reference, "://") {
		return ErrRecoveryRejected
	}
	if consume && (a.ExpiresMS <= now || p.ExpiresMS <= now) {
		return ErrRecoveryRejected
	}
	if !consume && (cert.ActivatedMS < p.CreatedMS || cert.ActivatedMS >= a.ExpiresMS || cert.ActivatedMS > now+5000) {
		return ErrRecoveryRejected
	}
	for i, s := range p.ManagedSources {
		if s.ApprovedSource != p.Sources[i] || s.Authority.ActorID != p.ActorID ||
			s.Authority.SourceStatus != "active" ||
			(s.Authority.AccessState != "active" && s.Authority.AccessState != "unmanaged") ||
			s.Authority.SourceID != s.SourceID || s.Authority.TenantID == 0 || s.Authority.KBID == "" ||
			!recoverySHA.MatchString(s.Authority.AuthorityHash) ||
			!recoverySHA.MatchString(s.ConfigSHA256) || !recoverySHA.MatchString(s.FactsSHA256) ||
			(i > 0 && s.SourceID <= p.ManagedSources[i-1].SourceID) {
			return ErrRecoveryRejected
		}
		if _, err := uuid.Parse(s.SourceID); err != nil {
			return ErrRecoveryRejected
		}
	}
	sig, err := hex.DecodeString(a.Signature)
	a.Signature = ""
	message, encodingErr := recoveryCanonical(a)
	if err != nil || encodingErr != nil || !ed25519.Verify(m.key, message, sig) {
		return ErrRecoveryRejected
	}
	return nil
}

func (m *RecoveryManager) checkSteadyState(ctx context.Context, state recoveryMode, now int64) error {
	if state.Version != 2 || state.Steady == nil || m.steadyAuthority == nil || state.Phase != "approved_sources" ||
		m.verifySteady(*state.Steady, now, false) != nil ||
		state.ContinuityID != state.Steady.Plan.ID || state.ExpiresMS != state.Steady.Plan.AuthorizationUntilMS ||
		state.Steady.Plan.Incarnation != m.controller.incarnation ||
		state.Steady.Plan.RuntimeSHA != m.controller.Config.SourceSHA ||
		state.Steady.Plan.WorkerID != m.controller.Config.WorkerID ||
		state.Steady.Plan.ControllerFingerprint != m.controller.fingerprint ||
		"feishu:safety:{"+state.Steady.Plan.Namespace+"}:" != m.controller.prefix ||
		state.Steady.Plan.MembershipRevision != state.MembershipRevision ||
		state.Steady.Plan.RecoveryPlanSHA256 != state.PlanSHA256 ||
		state.Steady.Plan.HistoricalQueueSHA256 != state.HistoricalQueueSHA256 ||
		len(state.ApprovedSources) != len(state.Steady.Plan.ManagedSources) {
		return ErrRecoveryRejected
	}
	for _, entry := range state.Steady.Plan.ManagedSources {
		if state.ApprovedSources[entry.SourceID] != entry.ApprovedSource {
			return ErrRecoveryRejected
		}
	}
	lease, err := m.controller.Client.Get(ctx, m.controller.prefix+"steady_health").Result()
	if err != nil || lease != state.ContinuityID {
		return ErrRecoveryRejected
	}
	return nil
}

// renewSteadyHealth runs only after the ordinary automatic controller has
// successfully proved its audit, topology and live consumers. XX prevents a
// lost/expired health proof from being silently recreated by an old approval.
func (c *Controller) renewSteadyHealth(ctx context.Context) error {
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var s recoveryMode
	if json.Unmarshal([]byte(raw), &s) != nil {
		return ErrRecoveryRejected
	}
	if s.Steady == nil {
		return nil
	}
	return c.Client.Eval(ctx, `
local t=redis.call('TIME');local ms=t[1]*1000+math.floor(t[2]/1000)
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[2] or
 redis.call('PTTL',KEYS[2])<=0 or redis.call('EXISTS',KEYS[3])~=0 or
 redis.call('GET',KEYS[4])~=ARGV[3] or redis.call('PTTL',KEYS[4])<=0 or
 tonumber(ARGV[4])<=ms then return redis.error_reply('STEADY_HEALTH_LOST') end
redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[5],'XX');return 1`,
		[]string{c.prefix + "recovery", c.prefix + "steady_health", c.prefix + "trip", c.Guard.PermitKey()},
		raw, s.ContinuityID, c.Guard.Fingerprint(), s.ExpiresMS, steadyHealthTTL.Milliseconds()).Err()
}

// ActivateSteady applies one independently signed exact replacement policy.
// The short-lived approval is consumed once; subsequent health renewal neither
// changes the source set nor extends the separately signed authorization end.
func (m *RecoveryManager) ActivateSteady(ctx context.Context, p SteadyPlan, a RecoveryApproval, actor string) error {
	if !m.operation.TryLock() {
		return ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	c := m.controller
	preflightNow, err := c.Client.Time(ctx).Result()
	preflight := SteadyCertificate{Plan: p, Approval: a, ActivatedMS: preflightNow.UnixMilli()}
	if err != nil || actor != p.ActorID || m.verifySteady(preflight, preflightNow.UnixMilli(), true) != nil {
		return ErrRecoveryRejected
	}
	entries := map[string]ApprovedSource{}
	for _, entry := range p.ManagedSources {
		current, facts, err := m.steadySource(ctx, entry.SourceID, actor)
		if err != nil || !sameSteadySource(current, entry, facts) || (!p.RevocationOnly &&
			(!facts.Eligible || facts.LiveExecutions != 0 || facts.LivePublications != 0)) {
			return ErrRecoveryRejected
		}
		entries[entry.SourceID] = entry.ApprovedSource
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := m.steadyReady(ctx); err != nil {
		return err
	}
	now, err := c.Client.Time(ctx).Result()
	cert := SteadyCertificate{Plan: p, Approval: a, ActivatedMS: now.UnixMilli()}
	if err != nil || actor != p.ActorID || m.verifySteady(cert, now.UnixMilli(), true) != nil ||
		p.WorkerID != c.Config.WorkerID || p.RuntimeSHA != c.Config.SourceSHA || p.Incarnation != c.incarnation ||
		p.ControllerFingerprint != c.fingerprint || "feishu:safety:{"+p.Namespace+"}:" != c.prefix ||
		c.departed || feishusafety.Default() != c.Guard {
		return ErrRecoveryRejected
	}
	if err := c.checkRecoveryPolicy(ctx); err != nil {
		return err
	}
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var state recoveryMode
	if err != nil || digestBytes(raw) != p.PreviousModeSHA256 || json.Unmarshal([]byte(raw), &state) != nil ||
		(state.Phase != "awaiting_review" && state.Phase != "approved_sources") ||
		state.PlanSHA256 != p.RecoveryPlanSHA256 || state.MembershipRevision != p.MembershipRevision {
		return ErrRecoveryRejected
	}
	expectedRevision := int64(1)
	if state.Steady != nil {
		expectedRevision = state.Steady.Plan.PolicyRevision + 1
	}
	if p.PolicyRevision != expectedRevision {
		return ErrRecoveryRejected
	}
	if state.Steady != nil && state.Steady.Plan.ActorID != actor {
		return ErrRecoveryRejected
	}
	if p.RevocationOnly {
		if state.Steady == nil || p.AuthorizationUntilMS != state.Steady.Plan.AuthorizationUntilMS ||
			len(p.ManagedSources) >= len(state.ApprovedSources) {
			return ErrRecoveryRejected
		}
		for _, entry := range p.ManagedSources {
			if state.ApprovedSources[entry.SourceID] != entry.ApprovedSource {
				return ErrRecoveryRejected
			}
		}
	}
	q, err := m.queue(ctx)
	if err != nil || q.digest != p.HistoricalQueueSHA256 || len(q.ids) != p.HistoricalPending {
		return ErrRecoveryRejected
	}
	if !p.RevocationOnly {
		if err := m.approvedQueueEmpty(ctx); err != nil {
			return err
		}
	}
	state.Steady = &cert
	// Old readers accept only version 1. A rollback must fail closed instead
	// of treating the authorization period as a long-lived legacy permit.
	state.Version = 2
	state.ApprovedSources = entries
	state.ContinuityID = p.ID
	state.Phase = "approved_sources"
	state.ExpiresMS = p.AuthorizationUntilMS
	state.HistoricalQueueSHA256 = p.HistoricalQueueSHA256
	state.HistoricalPending = p.HistoricalPending
	state.RunID = ""
	state.Generation = 0
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	signed, _ := json.Marshal(cert)
	// WATCH the same queue fields as incident recovery, without rewriting them.
	keys := []string{
		c.prefix + "recovery", c.prefix + "trip", c.prefix + "membership_revision",
		c.prefix + "workers", c.prefix + "leases", c.prefix + "admitted", c.Guard.PermitKey(),
	}
	for _, queue := range []string{"sync", ApprovedSyncQueue} {
		for _, key := range []string{"pending", "active", "retry", "scheduled", "archived", "paused"} {
			keys = append(keys, "asynq:{"+queue+"}:"+key)
		}
	}
	for _, id := range q.ids {
		keys = append(keys, "asynq:{sync}:t:"+id)
	}
	return c.Client.Watch(ctx, func(tx *redis.Tx) error {
		current, err := tx.Get(ctx, c.prefix+"recovery").Result()
		if err != nil || current != raw {
			return ErrRecoveryRejected
		}
		before, err := m.queue(ctx)
		if err != nil || before.digest != q.digest {
			return ErrRecoveryRejected
		}
		if !p.RevocationOnly {
			if err := m.approvedQueueEmpty(ctx); err != nil {
				return err
			}
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Eval(ctx, `
local t=redis.call('TIME');local ms=t[1]*1000+math.floor(t[2]/1000)
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[2])~=0 or
 redis.call('GET',KEYS[3])~=ARGV[2] or redis.call('PTTL',KEYS[3])<=0 or
 redis.call('GET',KEYS[6])~=ARGV[3] or redis.call('HLEN',KEYS[7])~=1 or
 redis.call('HGET',KEYS[7],ARGV[4])~=ARGV[5] or
 redis.call('HGET',KEYS[8],ARGV[4])~=ARGV[5] or
 tonumber(redis.call('ZSCORE',KEYS[9],ARGV[4]) or '0')<=ms or
 tonumber(ARGV[6])<=ms then return redis.error_reply('STEADY_ADMISSION_CHANGED') end
redis.call('XADD',KEYS[4],'*','event','steady_sources_activated','actor_id',ARGV[7],
 'certificate',ARGV[8],'historical_queue_sha256',ARGV[9])
redis.call('SET',KEYS[1],ARGV[10]);redis.call('SET',KEYS[5],ARGV[11],'PX',ARGV[12])
redis.call('DEL',KEYS[10]);return 1`,
				[]string{
					c.prefix + "recovery", c.prefix + "trip", c.Guard.PermitKey(), c.prefix + "control_audit",
					c.prefix + "steady_health", c.prefix + "membership_revision", c.prefix + "workers",
					c.prefix + "admitted", c.prefix + "leases", "asynq:{approved_sync}:paused",
				},
				raw, c.Guard.Fingerprint(), p.MembershipRevision, c.Config.WorkerID, c.fingerprint+":"+c.incarnation,
				a.ExpiresMS, actor, string(signed), q.digest, string(body), p.ID, steadyHealthTTL.Milliseconds())
			return nil
		})
		return err
	}, keys...)
}
