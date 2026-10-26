// Package feishusynccontrol provides the minimal Feishu rollout stop loop.
// It never resumes a queue, deletes tasks or changes datasource configuration.
package feishusynccontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Config selects a strict attested inventory or automatic Asynq/lease discovery.
type Config struct {
	Automatic       bool
	WorkerID        string
	ExpectedWorkers []string
	SourceSHA       string
	Scope           string
	Maintenance     int
}

// Metrics must be produced by an authorized monitoring identity. The key must
// have a TTL; absent fields, stale samples and unknown quota attestations close
// the gate. No account quota is inferred from local request counts.
type Metrics struct {
	ObservedUnixMS     int64   `json:"observed_unix_ms"`
	GatewayBudgetKnown *bool   `json:"gateway_budget_known"`
	GatewayHealthy     *bool   `json:"gateway_healthy"`
	OnlineHealthy      *bool   `json:"online_healthy"`
	ConsumerHealthy    *bool   `json:"consumer_healthy"`
	ScopeViolations    *int    `json:"scope_violations"`
	QuotaExceeded      *bool   `json:"quota_exceeded"`
	OnlineP95Ratio     float64 `json:"online_p95_ratio"`
}

// ErrNotReady means a rollout cannot safely issue an outbound permission.
var ErrNotReady = errors.New("feishu controller not ready")

// Event is content-free evidence; Audit must deliver it to the configured alert
// sink or return an error. Log-only wiring is not production alert verification.
type Event struct {
	StepID      string `json:",omitempty"`
	Incarnation string `json:",omitempty"`
	Kind        string
	Reason      string
	WorkerID    string
	Fenced      bool
	QueuePaused bool
	ProbeStage  string `json:",omitempty"`
	ElapsedMS   int64  `json:",omitempty"`
	ctx         context.Context
}

// Context carries the bounded control operation to its audit sink. Standalone
// diagnostic events retain the sink's existing fallback timeout.
func (e Event) Context() context.Context {
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// Controller validates inventory/telemetry and executes an idempotent stop loop.
type Controller struct {
	mu     sync.Mutex
	Client redis.UniversalClient
	Guard  *feishusafety.Guard
	Config Config
	Stop   func(context.Context) error
	Audit  func(Event) error
	// Consumers returns actual healthy Asynq sync consumers as host:pid IDs.
	// Automatic mode validates their registry leases, never synthesizes metrics.
	Consumers    func(context.Context) ([]string, error)
	prefix       string
	fingerprint  string
	incarnation  string
	departed     bool
	started      time.Time
	admitted     bool
	lastEvent    string
	lastAlertAck time.Time
	stepStarted  time.Time
	probeStage   string
	Transport    *ControlTransport
	Observe      func(StepTiming)
	timings      StepTiming
}

func (c *Controller) audit(ctx context.Context, event Event) error {
	event.StepID, event.Incarnation = c.timings.ID, c.incarnation
	event.ctx = ctx
	if err := c.Audit(event); err != nil {
		return err
	}
	// A custom sink may ignore cancellation. Never proceed to a new probe or
	// grant a permit after its acknowledged audit has exhausted this step.
	return ctx.Err()
}

// New preserves strict explicit-inventory mode. Automatic mode permits all or
// a canonical UUID list and validates discovered consumers on every step.
func New(client redis.UniversalClient, guard *feishusafety.Guard, cfg Config,
	stop func(context.Context) error, audit func(Event) error,
) (*Controller, error) {
	if client == nil || guard == nil || stop == nil || audit == nil || cfg.Maintenance < 1 ||
		(!cfg.Automatic && cfg.Maintenance != 1) {
		return nil, fmt.Errorf("%w: incomplete controller or canary concurrency", ErrNotReady)
	}
	if !cfg.Automatic && len(cfg.SourceSHA) != 40 {
		return nil, fmt.Errorf("%w: source identity", ErrNotReady)
	}
	if _, err := hex.DecodeString(cfg.SourceSHA); !cfg.Automatic && err != nil {
		return nil, fmt.Errorf("%w: source identity", ErrNotReady)
	}
	if cfg.Scope != "none" && (!cfg.Automatic || cfg.Scope != "all") {
		for _, entry := range strings.Split(cfg.Scope, ",") {
			id, err := uuid.Parse(strings.TrimSpace(entry))
			if err != nil || id.String() != strings.TrimSpace(entry) ||
				(!cfg.Automatic && strings.Contains(cfg.Scope, ",")) {
				return nil, fmt.Errorf("%w: scope must be none or one canonical UUID", ErrNotReady)
			}
		}
	}
	workers := append([]string(nil), cfg.ExpectedWorkers...)
	sort.Strings(workers)
	if (len(workers) == 0 && !cfg.Automatic) || len(workers) > 64 {
		return nil, fmt.Errorf("%w: expected inventory", ErrNotReady)
	}
	found := false
	for i, id := range workers {
		if id == "" || len(id) > 128 || strings.ContainsAny(id, " \t\r\n{}") || i > 0 && id == workers[i-1] {
			return nil, fmt.Errorf("%w: invalid worker identity", ErrNotReady)
		}
		found = found || id == cfg.WorkerID
	}
	if !found && !cfg.Automatic {
		return nil, fmt.Errorf("%w: worker absent from inventory", ErrNotReady)
	}
	cfg.ExpectedWorkers = workers
	if cfg.Automatic && (cfg.WorkerID == "" || strings.ContainsAny(cfg.WorkerID, " \t\r\n{}")) {
		return nil, fmt.Errorf("%w: worker identity", ErrNotReady)
	}
	identity := []any{cfg.SourceSHA, cfg.Scope, cfg.Maintenance, workers, guard.Fingerprint()}
	if cfg.Automatic {
		identity = []any{"automatic", cfg.SourceSHA, cfg.Scope, guard.Fingerprint()}
	}
	raw, _ := json.Marshal(identity)
	hash := sha256.Sum256(raw)
	return &Controller{
		Client: client, Guard: guard, Config: cfg, Stop: stop, Audit: audit,
		prefix: strings.TrimSuffix(guard.PermitKey(), "permit"), fingerprint: hex.EncodeToString(hash[:]),
		incarnation: uuid.NewString(), started: time.Now(),
	}, nil
}

// MetricsKey is the deployment-scoped monitoring input. Collectors must use the
// same Redis database and a restricted ACL, with a short TTL on every sample.
func (c *Controller) MetricsKey() string { return c.prefix + "metrics" }

var register = redis.NewScript(`
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
local old=redis.call('HGET',KEYS[1],ARGV[1]);local alive=redis.call('ZSCORE',KEYS[2],ARGV[1])
if old and old~=ARGV[2] and alive and tonumber(alive)>now then return redis.error_reply('WORKER_ID_CONFLICT') end
local admitted=redis.call('HGET',KEYS[4],ARGV[1])
if admitted and admitted~=ARGV[2] then return redis.error_reply('ADMITTED_INCARNATION_CONFLICT') end
if old~=ARGV[2] then
 redis.call('ZADD',KEYS[3],now+30000,ARGV[1]);redis.call('INCR',KEYS[5])
end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]);redis.call('ZADD',KEYS[2],now+2000,ARGV[1]);return now`)

var permit = redis.NewScript(`
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('EXISTS',KEYS[5])==1 then return redis.error_reply('TRIPPED') end
local ttl=redis.call('PTTL',KEYS[3])
if redis.call('GET',KEYS[3])~=ARGV[3] or ttl<=0 or ttl>2000 then
 return redis.error_reply('STALE_METRICS')
end
local sample=cjson.decode(ARGV[3])
if now-sample.observed_unix_ms>2000 then return redis.error_reply('STALE_METRICS') end
local active=redis.call('ZRANGEBYSCORE',KEYS[2],now,'+inf')
if #active~=#ARGV-3 then return redis.error_reply('INVENTORY_NOT_READY') end
for i=4,#ARGV do
 local value=redis.call('HGET',KEYS[1],ARGV[i])
 local untilms=redis.call('ZSCORE',KEYS[2],ARGV[i])
 if not value or not untilms or tonumber(untilms)<=now then return redis.error_reply('INVENTORY_NOT_READY') end
 if string.sub(value,1,64)~=ARGV[1] then return redis.error_reply('POLICY_MISMATCH') end
end
redis.call('SET',KEYS[4],ARGV[2],'PX',750);return 1`)

// Step registers this process, validates fresh metrics and atomically grants a
// 750ms permit. A 10s closed bootstrap window allows expected workers to join;
// it never resumes sync or bypasses outbound gating during startup.
func (c *Controller) Step(ctx context.Context) (result error) {
	requested := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stepStarted = time.Now()
	c.beginTiming(ctx)
	c.timings.Requested = requested
	c.timings.LockWaitNS = c.timings.Started.Sub(requested).Nanoseconds()
	defer func() { c.finishTiming(result) }()
	defer func() { c.probeStage = "" }()
	if c.departed {
		return ErrNotReady
	}
	now, err := register.Run(ctx, c.Client, []string{
		c.prefix + "workers", c.prefix + "leases", c.prefix + "joining",
		c.prefix + "admitted", c.prefix + "membership_revision",
	},
		c.Config.WorkerID, c.fingerprint+":"+c.incarnation).Int64()
	if err != nil {
		return c.freeze(ctx, "registry_"+failureClass(err), true)
	}
	if c.Config.Scope == "none" {
		return c.freeze(ctx, "scope_none", false)
	}
	if c.Config.Automatic {
		return c.automaticStep(ctx)
	}
	c.stage(ctx, "metrics")
	raw, err := c.Client.Get(ctx, c.MetricsKey()).Result()
	if err != nil {
		return c.freeze(ctx, "metrics_missing", c.admitted || time.Since(c.started) > 10*time.Second)
	}
	var metrics Metrics
	err = decodeMetrics(raw, &metrics)
	healthy := func(v *bool) bool { return v != nil && *v }
	if err != nil || !healthy(metrics.GatewayBudgetKnown) || !healthy(metrics.GatewayHealthy) ||
		!healthy(metrics.OnlineHealthy) || !healthy(metrics.ConsumerHealthy) ||
		metrics.ScopeViolations == nil || *metrics.ScopeViolations != 0 ||
		metrics.QuotaExceeded == nil || *metrics.QuotaExceeded || metrics.OnlineP95Ratio <= 0 ||
		metrics.OnlineP95Ratio > 1.2 || metrics.ObservedUnixMS > now+100 || now-metrics.ObservedUnixMS > 2000 {
		return c.freeze(ctx, "metrics_unhealthy_or_unknown", true)
	}
	if time.Since(c.lastAlertAck) > time.Second {
		heartbeat := Event{
			Kind:     "feishu_safety_heartbeat",
			Reason:   "controller_alive",
			WorkerID: c.Config.WorkerID,
		}
		c.stage(ctx, "audit")
		if err := c.audit(ctx, heartbeat); err != nil {
			return c.freeze(ctx, "alert_receiver_unavailable", true)
		}
		c.lastAlertAck = time.Now()
	}
	args := []any{c.fingerprint, c.Guard.Fingerprint(), raw}
	for _, id := range c.Config.ExpectedWorkers {
		args = append(args, id)
	}
	c.stage(ctx, "permit")
	err = permit.Run(ctx, c.Client, []string{
		c.prefix + "workers", c.prefix + "leases", c.MetricsKey(),
		c.Guard.PermitKey(), c.prefix + "trip",
	}, args...).Err()
	if err != nil {
		permanent := c.admitted || time.Since(c.started) > 10*time.Second ||
			strings.Contains(err.Error(), "MISMATCH") || strings.Contains(err.Error(), "TRIPPED")
		return c.freeze(ctx, "inventory_or_permit_invalid", permanent)
	}
	c.admitted = true
	return nil
}

func (c *Controller) freeze(ctx context.Context, reason string, latch bool) error {
	failedStage := c.probeStage
	elapsedMS := int64(0)
	if !c.stepStarted.IsZero() && c.probeStage != "" {
		elapsedMS = time.Since(c.stepStarted).Milliseconds()
	}
	// Monitoring timeouts must not cancel the independent stop/fencing work.
	// Outbound permit expires while a slower dependency stop is in progress.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.stage(stopCtx, "stop")
	ctx = stopCtx
	var failures []error
	if latch {
		failures = append(failures, c.Guard.Trip(ctx, reason))
	} else {
		failures = append(failures, c.Client.Del(ctx, c.Guard.PermitKey()).Err())
	}
	paused := c.Client.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err()
	failures = append(failures, paused)
	CancelLocal()
	fenced := c.Stop(ctx)
	failures = append(failures, fenced)
	event := Event{
		StepID: c.timings.ID, Incarnation: c.incarnation,
		Kind: "feishu_safety_stop", Reason: reason, WorkerID: c.Config.WorkerID,
		Fenced: fenced == nil, QueuePaused: paused == nil,
		ProbeStage: failedStage, ElapsedMS: elapsedMS,
	}
	// Repeated unsuccessful actions are reported, never misrepresented as done.
	if c.lastEvent != reason || paused != nil || fenced != nil {
		record := c.Client.XAdd(ctx, &redis.XAddArgs{
			Stream: c.prefix + "control_audit", MaxLen: 1000,
			Approx: true, Values: map[string]any{
				"event": event.Kind, "reason": event.Reason,
				"worker_id": event.WorkerID, "fenced": event.Fenced, "queue_paused": event.QueuePaused,
				"probe_stage": event.ProbeStage, "step_elapsed_ms": event.ElapsedMS,
				"step_id": event.StepID, "incarnation": event.Incarnation,
			},
		}).Err()
		alert := c.audit(ctx, event)
		failures = append(failures, record, alert)
		if record == nil && alert == nil {
			c.lastEvent = reason
		}
	}
	return errors.Join(append(failures, ErrNotReady)...)
}

// EmergencyStop is independent of all model/provider code. It cannot resume or
// clear a latch. The same stop sequence is retried by Run after Redis recovery.
func (c *Controller) EmergencyStop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.freeze(ctx, "operator_emergency_stop", true)
}

func decodeMetrics(raw string, metrics *Metrics) error {
	check := json.NewDecoder(strings.NewReader(raw))
	first, err := check.Token()
	if err != nil || first != json.Delim('{') {
		return ErrNotReady
	}
	seen := map[string]bool{}
	for check.More() {
		key, tokenErr := check.Token()
		name, ok := key.(string)
		if tokenErr != nil || !ok || seen[name] {
			return ErrNotReady
		}
		seen[name] = true
		var value json.RawMessage
		if err = check.Decode(&value); err != nil {
			return err
		}
	}
	if _, err = check.Token(); err != nil {
		return err
	}
	if _, err = check.Token(); err != io.EOF {
		return ErrNotReady
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(metrics)
}

// Run keeps monitoring and retries stop actions after dependency recovery.
func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		// Graceful lifecycle cancellation is handled by Drain below. Do not
		// misclassify that cancellation as Redis/metrics failure inside Step.
		stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
		_ = c.Step(stepCtx)
		cancel()
		select {
		case <-ctx.Done():
			if c.Config.Automatic {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.Drain(stopCtx)
				stopCancel()
			} else {
				CancelLocal()
			}
			return // controller loss leaves only a short-lived permit
		case <-ticker.C:
		}
	}
}

var local = struct {
	sync.Mutex
	cancels map[string]context.CancelFunc
}{cancels: map[string]context.CancelFunc{}}

// Track registers a marked legacy execution for cancellation on this process.
func Track(cancel context.CancelFunc) func() {
	id := uuid.NewString()
	local.Lock()
	local.cancels[id] = cancel
	local.Unlock()
	return func() { local.Lock(); delete(local.cancels, id); local.Unlock() }
}

// CancelLocal does not delete queue entries or alter datasource configuration.
func CancelLocal() {
	local.Lock()
	defer local.Unlock()
	for _, cancel := range local.cancels {
		cancel()
	}
}
