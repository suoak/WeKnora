package feishusynccontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrRecoveryRejected denotes a closed recovery precondition or approval.
var (
	ErrRecoveryRejected = errors.New("feishu_recovery_precondition_rejected")
	recoveryReference   = regexp.MustCompile(`^[A-Za-z0-9:._/-]{1,180}$`)
)

// RecoveryFacts is a content-free authoritative source snapshot.
type RecoveryFacts struct {
	SourceID              string `json:"source_id"`
	ConfigSHA256          string `json:"config_sha256"`
	FactsSHA256           string `json:"facts_sha256"`
	Eligible              bool   `json:"eligible"`
	RetryEligible         bool   `json:"retry_eligible,omitempty"`
	SelectedResourceCount int    `json:"selected_resource_count"`
	LiveExecutions        int64  `json:"live_executions"`
	LivePublications      int64  `json:"live_publications"`
}

// RecoveryPlan binds a short approval window to one incident and source.
type RecoveryPlan struct {
	ApprovalKeyID         string        `json:"approval_key_id"`
	Version               int           `json:"version"`
	PlanID                string        `json:"plan_id"`
	ActorID               string        `json:"actor_id"`
	Namespace             string        `json:"namespace"`
	AppSHA                string        `json:"app_sha"`
	Trip                  string        `json:"trip"`
	IncidentID            string        `json:"incident_id"`
	WorkerID              string        `json:"worker_id"`
	Incarnation           string        `json:"incarnation"`
	ControllerFingerprint string        `json:"controller_fingerprint"`
	MembershipRevision    string        `json:"membership_revision"`
	QueueSHA256           string        `json:"queue_sha256"`
	QueuePendingCount     int           `json:"queue_pending_count"`
	PreviousModeSHA256    string        `json:"previous_mode_sha256"`
	Facts                 RecoveryFacts `json:"facts"`
	CreatedMS             int64         `json:"created_ms"`
	ExpiresMS             int64         `json:"expires_ms"`
}

// RecoveryApproval is independent from the plan. The offline approver attests that
// the referenced backup/restore and isolation evidence has been reviewed.
// The server does not pretend a 64-character digest proves those tests ran.
type RecoveryApproval struct {
	Version        int    `json:"version"`
	Algorithm      string `json:"algorithm"`
	KeyID          string `json:"key_id"`
	Incarnation    string `json:"incarnation"`
	Operation      string `json:"operation"`
	PlanSHA256     string `json:"plan_sha256"`
	ActorID        string `json:"actor_id"`
	Reference      string `json:"reference"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	ExpiresMS      int64  `json:"expires_ms"`
	Signature      string `json:"signature"`
}

type recoveryMode struct {
	ContinuityID          string                    `json:"continuity_id,omitempty"`
	ApprovedSources       map[string]ApprovedSource `json:"approved_sources,omitempty"`
	RuntimeSHA            string                    `json:"runtime_sha"`
	WorkerID              string                    `json:"worker_id"`
	Incarnation           string                    `json:"incarnation"`
	ControllerFingerprint string                    `json:"controller_fingerprint"`
	MembershipRevision    string                    `json:"membership_revision"`
	Version               int                       `json:"version"`
	PlanID                string                    `json:"plan_id"`
	PlanSHA256            string                    `json:"plan_sha256"`
	SourceID              string                    `json:"source_id"`
	ConfigSHA256          string                    `json:"config_sha256"`
	Phase                 string                    `json:"phase"`
	RunID                 string                    `json:"run_id"`
	Generation            int64                     `json:"generation"`
	ExpiresMS             int64                     `json:"expires_ms"`
}

// RecoveryManager coordinates one approved replacement Guard lifecycle.
type RecoveryManager struct {
	controller *Controller
	operation  sync.Mutex
	key        ed25519.PublicKey
	keyID      string
	facts      func(context.Context, string) (RecoveryFacts, error)
}

var recoveryManager atomic.Pointer[RecoveryManager]

// Recovery returns the configured operational recovery manager.
func Recovery() *RecoveryManager { return recoveryManager.Load() }

// InstallRecovery installs or clears operational recovery at startup/shutdown.
func InstallRecovery(m *RecoveryManager) { recoveryManager.Store(m) }

// NewRecoveryManager requires automatic control, independent approval key and authority loader.
func NewRecoveryManager(
	c *Controller,
	key []byte,
	facts func(context.Context, string) (RecoveryFacts, error),
) (*RecoveryManager, error) {
	if c == nil || !c.Config.Automatic || len(key) != ed25519.PublicKeySize || facts == nil {
		return nil, ErrRecoveryRejected
	}
	c.recoveryEnabled = true
	// A recovery-configured replacement process must not interpret complete
	// Redis state loss as a clean first installation and reopen normal traffic.
	// The requirement survives in the local Guard even when all Redis markers
	// are absent; a newly observed incident needs its own matching approval.
	c.recoveryRequired = true
	c.Guard.RequireRecovery()
	c.approvedFacts = facts
	m := &RecoveryManager{
		controller: c,
		key:        append(ed25519.PublicKey(nil), key...),
		keyID:      RecoveryVerificationKeyID(key),
		facts:      facts,
	}
	c.Guard.SetApprovedSourceValidator(m.checkApprovedSource)
	return m, nil
}

func (c *Controller) checkRecoveryPolicy(ctx context.Context) error {
	raw, e := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if errors.Is(e, redis.Nil) {
		required, err := c.Client.Exists(ctx, c.prefix+"recovery_required").Result()
		if err != nil || required != 0 || c.recoveryRequired {
			return ErrRecoveryRejected
		}
		return nil
	}
	if e != nil {
		return e
	}
	required, requiredErr := c.Client.Get(ctx, c.prefix+"recovery_required").Result()
	var state recoveryMode
	if requiredErr != nil {
		return ErrRecoveryRejected
	}
	if json.Unmarshal([]byte(raw), &state) != nil || state.Version != 1 ||
		state.PlanSHA256 != required ||
		state.RuntimeSHA != c.Config.SourceSHA ||
		state.WorkerID != c.Config.WorkerID ||
		state.Incarnation != c.incarnation ||
		state.ControllerFingerprint != c.fingerprint {
		return ErrRecoveryRejected
	}
	now, e := c.Client.Time(ctx).Result()
	if e != nil {
		return e
	}
	if state.ExpiresMS <= now.UnixMilli() {
		return ErrRecoveryRejected
	}
	if state.Phase == "approved_sources" {
		if c.approvedFacts == nil || state.ContinuityID == "" || len(state.ApprovedSources) < 1 ||
			len(state.ApprovedSources) > 4 {
			return ErrRecoveryRejected
		}
		if c.ApprovedConsumers == nil {
			return ErrRecoveryRejected
		}
		consumers, err := c.ApprovedConsumers(ctx)
		if err != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
			return ErrRecoveryRejected
		}
		if n, err := c.Client.Exists(ctx, "asynq:{approved_sync}:paused").Result(); err != nil ||
			n != 0 {
			return ErrRecoveryRejected
		}
		for id, approved := range state.ApprovedSources {
			facts, err := c.approvedFacts(ctx, id)
			if err != nil || !approved.matches(facts) {
				return ErrRecoveryRejected
			}
		}
	}
	return nil
}

func recoveryCanonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var object any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&object); err != nil {
		return nil, err
	}
	return json.Marshal(object)
}

// RecoveryDigest hashes canonical JSON for reviewable plan and evidence binding.
func RecoveryDigest(v any) (string, error) {
	b, e := recoveryCanonical(v)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func digestBytes(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (m *RecoveryManager) verify(p RecoveryPlan, a RecoveryApproval, actor, op string) error {
	now := time.Now().UnixMilli()
	if m == nil ||
		p.CreatedMS > now+5000 ||
		p.ExpiresMS-now > int64((15*time.Minute)/time.Millisecond) ||
		a.ExpiresMS-now > int64((15*time.Minute)/time.Millisecond) ||
		p.ExpiresMS <= p.CreatedMS ||
		p.Version != 1 || p.ApprovalKeyID != m.keyID ||
		a.Version != 2 ||
		a.Algorithm != "Ed25519" || a.KeyID != m.keyID ||
		a.Operation != op || a.Incarnation == "" ||
		(op == "reset" && a.Incarnation != p.Incarnation) ||
		a.ActorID != actor ||
		p.ActorID != actor ||
		actor == "" || !recoveryReference.MatchString(a.Reference) || strings.Contains(a.Reference, "://") ||
		len(a.EvidenceSHA256) != 64 ||
		a.ExpiresMS > p.ExpiresMS ||
		a.ExpiresMS <= time.Now().UnixMilli() ||
		p.ExpiresMS <= time.Now().UnixMilli() ||
		p.ExpiresMS-p.CreatedMS > int64((15*time.Minute)/time.Millisecond) {
		return ErrRecoveryRejected
	}
	if _, err := hex.DecodeString(a.EvidenceSHA256); err != nil {
		return ErrRecoveryRejected
	}
	h, err := RecoveryDigest(p)
	if err != nil || h != a.PlanSHA256 {
		return ErrRecoveryRejected
	}
	sig, err := hex.DecodeString(a.Signature)
	if err != nil {
		return ErrRecoveryRejected
	}
	a.Signature = ""
	body, err := recoveryCanonical(a)
	if err != nil {
		return err
	}
	if !ed25519.Verify(m.key, body, sig) {
		return ErrRecoveryRejected
	}
	return nil
}

type recoveryQueueSnapshot struct {
	ids    []string
	fields []map[string]string
	digest string
	pause  string
}

func (m *RecoveryManager) queue(ctx context.Context) (recoveryQueueSnapshot, error) {
	c := m.controller.Client
	var q recoveryQueueSnapshot
	paused, e := c.Exists(ctx, "asynq:{sync}:paused").Result()
	if e != nil || paused != 1 {
		return q, ErrRecoveryRejected
	}
	q.pause, e = c.Get(ctx, "asynq:{sync}:paused").Result()
	if e != nil {
		return q, e
	}
	for _, key := range []string{"active", "retry", "scheduled", "archived"} {
		var n int64
		if key == "active" {
			n, e = c.LLen(ctx, "asynq:{sync}:"+key).Result()
		} else {
			n, e = c.ZCard(ctx, "asynq:{sync}:"+key).Result()
		}
		if e != nil || n != 0 {
			return q, ErrRecoveryRejected
		}
	}
	q.ids, e = c.LRange(ctx, "asynq:{sync}:pending", 0, 64).Result()
	if e != nil || len(q.ids) > 64 {
		return q, ErrRecoveryRejected
	}
	var hashes []map[string]string
	for _, id := range q.ids {
		f, e := c.HGetAll(ctx, "asynq:{sync}:t:"+id).Result()
		if e != nil || len(f) == 0 {
			return q, ErrRecoveryRejected
		}
		q.fields = append(q.fields, f)
		h := map[string]string{}
		for k, v := range f {
			h[k] = digestBytes(v)
		}
		hashes = append(hashes, h)
	}
	q.digest, e = RecoveryDigest([]any{q.pause, q.ids, hashes})
	return q, e
}

// Plan reads and binds the current stopped incident without changing it.
func (m *RecoveryManager) Plan(ctx context.Context, source, actor string) (RecoveryPlan, error) {
	var p RecoveryPlan
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	f, e := m.facts(ctx, source)
	if e != nil || !f.Eligible || f.SelectedResourceCount < 1 || f.SelectedResourceCount > 64 ||
		f.SourceID != source ||
		f.LiveExecutions != 0 ||
		f.LivePublications != 0 {
		return p, ErrRecoveryRejected
	}
	q, e := m.queue(ctx)
	if e != nil {
		return p, e
	}
	trip, e := c.Client.Get(ctx, c.prefix+"trip").Result()
	if e != nil || trip == "" {
		return p, ErrRecoveryRejected
	}
	permit, e := c.Client.Exists(ctx, c.Guard.PermitKey()).Result()
	if e != nil || permit != 0 {
		return p, ErrRecoveryRejected
	}
	rows, e := c.Client.XRevRangeN(ctx, c.prefix+"audit", "+", "-", 1).Result()
	if e != nil || len(rows) != 1 || rows[0].Values["event"] != "trip" ||
		rows[0].Values["reason"] != trip {
		return p, ErrRecoveryRejected
	}
	revision, e := c.Client.Get(ctx, c.prefix+"membership_revision").Result()
	if e != nil {
		return p, e
	}
	workers, e := c.Client.HGetAll(ctx, c.prefix+"workers").Result()
	if e != nil || len(workers) != 1 ||
		workers[c.Config.WorkerID] != c.fingerprint+":"+c.incarnation {
		return p, ErrRecoveryRejected
	}
	mode, e := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if e != nil && !errors.Is(e, redis.Nil) {
		return p, e
	}
	now := time.Now().UnixMilli()
	p = RecoveryPlan{
		ApprovalKeyID: m.keyID,
		Version:       1,
		PlanID:        uuid.NewString(),
		ActorID:       actor,
		Namespace: strings.TrimSuffix(
			strings.TrimPrefix(c.prefix, "feishu:safety:{"),
			"}:",
		),
		AppSHA:                c.Config.SourceSHA,
		Trip:                  trip,
		IncidentID:            rows[0].ID,
		WorkerID:              c.Config.WorkerID,
		Incarnation:           c.incarnation,
		ControllerFingerprint: c.fingerprint,
		MembershipRevision:    revision,
		QueueSHA256:           q.digest,
		QueuePendingCount:     len(q.ids),
		PreviousModeSHA256:    digestBytes(mode),
		Facts:                 f,
		CreatedMS:             now,
		ExpiresMS:             now + int64((15*time.Minute)/time.Millisecond),
	}
	return p, nil
}

const recoveryQueueCAS = `
local expected=cjson.decode(ARGV[8]);local ids=redis.call('LRANGE','asynq:{sync}:pending',0,-1)
if #ids~=#expected then return redis.error_reply('RECOVERY_PENDING_CHANGED') end
local pos=9
for i,id in ipairs(ids) do
 if id~=expected[i] then return redis.error_reply('RECOVERY_PENDING_CHANGED') end
 local key='asynq:{sync}:t:'..id;local count=tonumber(ARGV[pos]);pos=pos+1
 if redis.call('HLEN',key)~=count then return redis.error_reply('RECOVERY_TASK_CHANGED') end
 for j=1,count do
  if redis.call('HGET',key,ARGV[pos])~=ARGV[pos+1] then return redis.error_reply('RECOVERY_TASK_CHANGED') end
  pos=pos+2
 end
end
if redis.call('EXISTS','asynq:{sync}:paused')~=1 or redis.call('LLEN','asynq:{sync}:active')~=0 or
 redis.call('ZCARD','asynq:{sync}:retry')~=0 or redis.call('ZCARD','asynq:{sync}:scheduled')~=0 or
 redis.call('ZCARD','asynq:{sync}:archived')~=0 then return redis.error_reply('RECOVERY_QUEUE_CHANGED') end
`

// Reset replaces a single, drained local Guard lifecycle. Redis holds the
// recovery state persistently: expiry closes permission, never reopens all.
func (m *RecoveryManager) Reset(
	ctx context.Context,
	p RecoveryPlan,
	a RecoveryApproval,
	actor string,
) error {
	if !m.operation.TryLock() {
		return ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	if e := m.verify(p, a, actor, "reset"); e != nil {
		return e
	}
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.AppSHA != c.Config.SourceSHA || p.WorkerID != c.Config.WorkerID ||
		p.Incarnation != c.incarnation ||
		p.ControllerFingerprint != c.fingerprint ||
		"feishu:safety:{"+p.Namespace+"}:" != c.prefix ||
		c.departed ||
		feishusafety.Default() != c.Guard {
		return ErrRecoveryRejected
	}
	f, e := m.facts(ctx, p.Facts.SourceID)
	if e != nil || f != p.Facts || !f.Eligible {
		return ErrRecoveryRejected
	}
	q, e := m.queue(ctx)
	if e != nil || (q.digest != p.QueueSHA256 || len(q.ids) != p.QueuePendingCount) {
		return ErrRecoveryRejected
	}
	previous, e := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if e != nil && !errors.Is(e, redis.Nil) {
		return e
	}
	if digestBytes(previous) != p.PreviousModeSHA256 {
		return ErrRecoveryRejected
	}
	if c.Consumers == nil {
		return ErrRecoveryRejected
	}
	consumers, consumerErr := c.Consumers(ctx)
	if consumerErr != nil || len(consumers) != 1 || consumers[0] != c.Config.WorkerID {
		return ErrRecoveryRejected
	}
	c.Guard.CloseLocal()
	CancelLocal()
	if e = c.Stop(ctx); e != nil {
		return e
	}
	local.Lock()
	tracked := len(local.cancels)
	local.Unlock()
	if tracked != 0 {
		return ErrRecoveryRejected
	}
	f, e = m.facts(ctx, p.Facts.SourceID)
	if e != nil || f != p.Facts {
		return ErrRecoveryRejected
	}
	fresh, e := c.Guard.Fresh()
	if e != nil {
		return e
	}
	inc := uuid.NewString()
	planHash, _ := RecoveryDigest(p)
	state, _ := json.Marshal(
		recoveryMode{
			Version:               1,
			RuntimeSHA:            c.Config.SourceSHA,
			WorkerID:              c.Config.WorkerID,
			Incarnation:           inc,
			ControllerFingerprint: c.fingerprint,

			PlanID:       p.PlanID,
			PlanSHA256:   planHash,
			SourceID:     f.SourceID,
			ConfigSHA256: f.ConfigSHA256,
			Phase:        "frozen",
			ExpiresMS:    p.ExpiresMS,
		},
	)
	ids, _ := json.Marshal(q.ids)
	signedApproval, _ := json.Marshal(a)
	approvalAudit, _ := json.Marshal(
		map[string]string{
			"actor_id":        actor,
			"reference":       a.Reference,
			"evidence_sha256": a.EvidenceSHA256,
			"key_id":          a.KeyID,
			"algorithm":       a.Algorithm,
			"signed_approval": string(signedApproval),
		},
	)
	args := []any{
		p.Trip,
		p.IncidentID,
		p.MembershipRevision,
		c.fingerprint + ":" + c.incarnation,
		c.fingerprint + ":" + inc,
		string(state),
		previous,
		string(ids),
	}
	for _, fields := range q.fields {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		args = append(args, len(keys))
		for _, k := range keys {
			args = append(args, k, []byte(fields[k]))
		}
	}
	script := redis.NewScript(recoveryQueueCAS + `
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[10])~=0 or
 redis.call('GET','asynq:{sync}:paused')~=ARGV[#ARGV-3] or redis.call('GET',KEYS[5])~=ARGV[3] or
 redis.call('HLEN',KEYS[3])~=1 or redis.call('HGET',KEYS[3],ARGV[#ARGV-1])~=ARGV[4] or
 redis.call('HLEN',KEYS[6])~=0 or (redis.call('GET',KEYS[8]) or '')~=ARGV[7] then return
redis.error_reply('RECOVERY_INCIDENT_CHANGED') end
local event=redis.call('XREVRANGE',KEYS[2],'+','-','COUNT',1)
if #event~=1 or event[1][1]~=ARGV[2] then return redis.error_reply('RECOVERY_INCIDENT_CHANGED') end
local leases=redis.call('ZRANGE',KEYS[4],0,-1)
if #leases~=1 or leases[1]~=ARGV[#ARGV-1] then return redis.error_reply('RECOVERY_TOPOLOGY_CHANGED') end
local now=redis.call('TIME');local ms=now[1]*1000+math.floor(now[2]/1000)
local state=cjson.decode(ARGV[6]);if state.expires_ms<=ms or not tonumber(ARGV[3]) then return
redis.error_reply('RECOVERY_EXPIRED') end
if tonumber(redis.call('ZSCORE',KEYS[4],leases[1]) or '0')<=ms then return
redis.error_reply('RECOVERY_LEASE_LOST') end
local admitted=redis.call('HGETALL',KEYS[9])
if #admitted>2 or (#admitted==2 and (admitted[1]~=ARGV[#ARGV-1] or admitted[2]~=ARGV[4])) then return
redis.error_reply('RECOVERY_TOPOLOGY_CHANGED') end
local approval=cjson.decode(ARGV[#ARGV-2])
redis.call('XADD',KEYS[7],'*','event','recovery_reset','plan_sha256',ARGV[#ARGV],'incident_id',ARGV[2],
'actor_id',
approval.actor_id,
'approval_reference',
approval.reference,
'evidence_sha256',
approval.evidence_sha256,
'approval_key_id',
approval.key_id,
'approval_algorithm',
approval.algorithm,
'signed_approval',
approval.signed_approval,
'worker_id',
state.worker_id,
'incarnation',
state.incarnation)
redis.call('SET',KEYS[11],ARGV[#ARGV])
state.membership_revision=tostring(tonumber(ARGV[3])+1)
redis.call('SET',KEYS[8],cjson.encode(state))
redis.call('HSET',KEYS[3],ARGV[#ARGV-1],ARGV[5])
redis.call('HSET',KEYS[9],ARGV[#ARGV-1],ARGV[5])
redis.call('ZADD',KEYS[4],ms+2000,ARGV[#ARGV-1]);redis.call('INCR',KEYS[5])
redis.call('DEL',KEYS[1]);return 1`)
	args = append(args, q.pause, string(approvalAudit), p.WorkerID, planHash)
	e = script.Eval(ctx,
		c.Client,
		[]string{
			c.prefix + "trip",
			c.prefix + "audit",
			c.prefix + "workers",
			c.prefix + "leases",
			c.prefix + "membership_revision",
			c.prefix + "inflight",
			c.prefix + "control_audit",
			c.prefix + "recovery",
			c.prefix + "admitted",
			c.Guard.PermitKey(),
			c.prefix + "recovery_required",
		},
		args...).
		Err()
	if e != nil {
		return fmt.Errorf("%w: reset unconfirmed", ErrRecoveryRejected)
	}
	fresh.RequireRecovery()
	c.recoveryRequired = true
	c.Guard = fresh
	c.incarnation = inc
	c.admitted = false
	c.lastEvent = ""
	c.lastAlertAck = time.Time{}
	feishusafety.InstallDefault(fresh)
	return nil
}

func (m *RecoveryManager) transition(
	ctx context.Context,
	p RecoveryPlan,
	a RecoveryApproval,
	actor, operation, from, to, run string,
	generation int64,
) error {
	if e := m.verify(p, a, actor, operation); e != nil {
		return e
	}
	f, e := m.facts(ctx, p.Facts.SourceID)
	if e != nil || !f.Eligible || f.ConfigSHA256 != p.Facts.ConfigSHA256 ||
		f.FactsSHA256 != p.Facts.FactsSHA256 {
		return ErrRecoveryRejected
	}

	q, e := m.queue(ctx)
	if e != nil || (q.digest != p.QueueSHA256 || len(q.ids) != p.QueuePendingCount) {
		return ErrRecoveryRejected
	}
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Config.SourceSHA != p.AppSHA || c.Config.WorkerID != p.WorkerID ||
		c.fingerprint != p.ControllerFingerprint ||
		c.incarnation != a.Incarnation ||
		c.departed ||
		feishusafety.Default() != c.Guard {
		return ErrRecoveryRejected
	}
	h, _ := RecoveryDigest(p)
	ids, _ := json.Marshal(q.ids)
	args := []any{h, from, to, run, generation, a.Reference, a.ExpiresMS, string(ids)}
	for _, fields := range q.fields {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		args = append(args, len(keys))
		for _, k := range keys {
			args = append(args, k, []byte(fields[k]))
		}
	}
	signedApproval, _ := json.Marshal(a)
	approvalAudit, _ := json.Marshal(
		map[string]string{
			"actor_id":        actor,
			"evidence_sha256": a.EvidenceSHA256,
			"key_id":          a.KeyID,
			"algorithm":       a.Algorithm,
			"signed_approval": string(signedApproval),
			"incarnation":     a.Incarnation,
			"runtime_sha":     c.Config.SourceSHA,
			"worker_id":       c.Config.WorkerID,
			"fingerprint":     c.fingerprint,
		},
	)
	args = append(args, string(approvalAudit), m.controller.Guard.Fingerprint(), q.pause)
	return redis.NewScript(recoveryQueueCAS+`
if redis.call('EXISTS',KEYS[1])==1 or redis.call('GET','asynq:{sync}:paused')~=ARGV[#ARGV] or
 redis.call('GET',KEYS[5])~=ARGV[#ARGV-1] or redis.call('PTTL',KEYS[5])<=0 then return
redis.error_reply('RECOVERY_CLOSED') end
local raw=redis.call('GET',KEYS[2]);if not raw then return redis.error_reply('RECOVERY_MISSING') end
local s=cjson.decode(raw);local t=redis.call('TIME');local ms=t[1]*1000+math.floor(t[2]/1000)
if redis.call('GET',KEYS[4])~=ARGV[1] or s.version~=1 or s.plan_sha256~=ARGV[1] or s.phase~=ARGV[2] or
s.expires_ms<=ms then return redis.error_reply('RECOVERY_STATE_CHANGED') end
local approval=cjson.decode(ARGV[#ARGV-2])
if s.runtime_sha~=approval.runtime_sha or s.worker_id~=approval.worker_id or
 s.incarnation~=approval.incarnation or s.controller_fingerprint~=approval.fingerprint or
 (redis.call('GET',KEYS[8]) or '')~=s.membership_revision or
 redis.call('HGET',KEYS[6],s.worker_id)~=s.controller_fingerprint..':'..s.incarnation or
 redis.call('HGET',KEYS[7],s.worker_id)~=s.controller_fingerprint..':'..s.incarnation or
 tonumber(redis.call('ZSCORE',KEYS[9],s.worker_id) or '0')<=ms then
 return redis.error_reply('RECOVERY_EXECUTION_IDENTITY_CHANGED') end
redis.call('XADD',KEYS[3],'*','event','recovery_'..ARGV[3],'plan_sha256',ARGV[1],'approval_reference',ARGV[6],
'approval_key_id',approval.key_id,'approval_algorithm',approval.algorithm,'signed_approval',approval.signed_approval,
'actor_id',
approval.actor_id,
'evidence_sha256',
approval.evidence_sha256,
'source_id',
s.source_id,
'run_id',
ARGV[4],
'generation',
ARGV[5])
s.phase=ARGV[3];s.run_id=ARGV[4];s.generation=tonumber(ARGV[5]);s.expires_ms=math.min(s.expires_ms,tonumber(ARGV[7]));
redis.call('SET',KEYS[2],cjson.encode(s));return 1`).Eval(ctx, m.controller.Client,
		[]string{
			m.controller.prefix + "trip",
			m.controller.prefix + "recovery",
			m.controller.prefix + "control_audit",
			m.controller.prefix + "recovery_required",
			m.controller.Guard.PermitKey(),
			c.prefix + "workers", c.prefix + "admitted", c.prefix + "membership_revision", c.prefix + "leases",
		}, args...).
		Err()
}

// Canary executes exactly one normal service callback. It never touches the
// original queue. Bind is called at durable admission, before ProcessSync Claim.
func (m *RecoveryManager) Canary(
	ctx context.Context,
	p RecoveryPlan,
	a RecoveryApproval,
	actor string,
	execute func(context.Context, func(string, int64) error) error,
) (err error) {
	if !m.operation.TryLock() {
		return ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	if execute == nil {
		return ErrRecoveryRejected
	}
	permit, e := m.controller.Client.Get(ctx, m.controller.Guard.PermitKey()).Result()
	if e != nil || permit != m.controller.Guard.Fingerprint() {
		return ErrRecoveryRejected
	}
	q, e := m.queue(ctx)
	if e != nil || (q.digest != p.QueueSHA256 || len(q.ids) != p.QueuePendingCount) {
		return ErrRecoveryRejected
	}
	if e = m.transition(ctx, p, a, actor, "canary", "frozen", "admit", "", 0); e != nil {
		return e
	}
	work, cancel := context.WithDeadline(ctx, time.UnixMilli(a.ExpiresMS))
	defer cancel()
	work = feishusafety.WithRecoveryContext(
		work,
		feishusafety.RecoveryContext{Plan: p.PlanID, Config: p.Facts.ConfigSHA256},
	)
	var bound atomic.Bool
	watchDone := make(chan struct{})
	watchStopped := make(chan struct{})
	watchFailure := make(chan error, 1)
	go func() {
		defer close(watchStopped)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-work.Done():
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(work, time.Second)
				facts, probeErr := m.facts(probe, p.Facts.SourceID)
				if probeErr == nil &&
					(!facts.Eligible ||
						facts.LiveExecutions > 1 ||
						facts.LivePublications > 1 ||
						facts.ConfigSHA256 != p.Facts.ConfigSHA256 ||
						facts.FactsSHA256 != p.Facts.FactsSHA256) {
					probeErr = ErrRecoveryRejected
				}
				if probeErr == nil {
					snapshot, queueErr := m.queue(probe)
					tripped, tripErr := m.controller.Client.Exists(probe, m.controller.prefix+"trip").
						Result()
					if queueErr != nil || tripErr != nil || tripped != 0 ||
						snapshot.digest != p.QueueSHA256 {
						probeErr = ErrRecoveryRejected
					}
				}
				stop()
				if probeErr != nil {
					watchFailure <- ErrRecoveryRejected
					cancel()
					stopCtx, finish := context.WithTimeout(
						context.WithoutCancel(ctx),
						5*time.Second,
					)
					_ = m.controller.EmergencyStop(stopCtx)
					finish()
					return
				}
			}
		}
	}()
	defer func() {
		panicked := recover()
		close(watchDone)
		cancel()
		<-watchStopped
		select {
		case failure := <-watchFailure:
			err = failure
		default:
		}
		if panicked != nil {
			err = ErrRecoveryRejected
		}
		if err == nil && !bound.Load() {
			err = ErrRecoveryRejected
		}
		if err == nil {
			probe, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			q, e := m.queue(probe)
			finish()
			if e != nil || (q.digest != p.QueueSHA256 || len(q.ids) != p.QueuePendingCount) {
				err = ErrRecoveryRejected
			}
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err == nil {
			err = m.transition(cleanup, p, a, actor, "canary", "running", "awaiting_review", "", 0)
		}
		if err != nil {
			_ = m.controller.EmergencyStop(cleanup)
		}
		if panicked != nil {
			panic(panicked)
		}
	}()
	return execute(work, func(run string, generation int64) error {
		if _, e := uuid.Parse(run); e != nil || generation != 1 ||
			!bound.CompareAndSwap(false, true) {
			return ErrRecoveryRejected
		}
		return m.transition(work, p, a, actor, "canary", "admit", "running", run, generation)
	})
}

// Status reports only the requested source recovery state without mutating it.
func (m *RecoveryManager) Status(ctx context.Context, source string) (map[string]any, error) {
	c := m.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, e := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if e != nil {
		return nil, e
	}
	var state recoveryMode
	if json.Unmarshal([]byte(raw), &state) != nil || state.SourceID != source {
		return nil, ErrRecoveryRejected
	}
	// Do not expose other sources from a multi-source policy through a route
	// that has authorized only this source. Copy before redacting the map.
	if state.ApprovedSources != nil {
		entry, ok := state.ApprovedSources[source]
		state.ApprovedSources = nil
		if ok {
			state.ApprovedSources = map[string]ApprovedSource{source: entry}
		}
	}
	trip, e := c.Client.Exists(ctx, c.prefix+"trip").Result()
	if e != nil {
		return nil, e
	}
	permit, e := c.Client.Get(ctx, c.Guard.PermitKey()).Result()
	if e != nil && !errors.Is(e, redis.Nil) {
		return nil, e
	}
	ttl, e := c.Client.PTTL(ctx, c.Guard.PermitKey()).Result()
	if e != nil {
		return nil, e
	}
	paused, e := c.Client.Exists(ctx, "asynq:{sync}:paused").Result()
	if e != nil {
		return nil, e
	}
	return map[string]any{
		"state": state, "trip_latched": trip != 0, "sync_paused": paused == 1,
		"permit_ready": trip == 0 && paused == 1 && ttl > 0 &&
			permit == c.Guard.Fingerprint(), "permit_ttl_ms": ttl.Milliseconds(),
	}, nil
}

// RecoveryVerificationKeyID identifies the trusted public key without carrying
// a signing capability or accepting an algorithm supplied by a caller.
func RecoveryVerificationKeyID(key []byte) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:])
}
