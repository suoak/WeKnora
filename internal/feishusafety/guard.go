// Package feishusafety implements fail-closed admission for explicitly marked
// legacy Feishu background work. It does not govern interactive model traffic.
package feishusafety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	// ErrClosed indicates that the distributed safety gate cannot allow work.
	ErrClosed = errors.New("feishu safety gate closed")
	// ErrBudgetBusy indicates that a known local allocation is temporarily full.
	ErrBudgetBusy = errors.New("feishu safety budget busy")
)

// Config budgets belong to ONE shared gateway allocation, not to each worker.
// TokenBoundVerified requires an administrator-verified tokenizer contract:
// input tokens <= UTF-8 input bytes + TokenOverhead per input. Unknown contracts
// must not activate the gate. This does not assert that the gateway has capacity.
type Config struct {
	Namespace          string
	EndpointHost       string
	InFlight           int
	RPM                int
	RPS                int
	TPM                int
	WaitBudgetDuration time.Duration
	TokenBoundVerified bool
	TokenOverhead      int
}

// Guard coordinates a deployment-scoped, fail-closed Redis safety allocation.
type Guard struct {
	client      redis.UniversalClient
	config      Config
	fingerprint string
	prefix      string
	localClosed atomic.Bool
}

// New validates explicit budgets and constructs a closed-by-default guard.
func New(client redis.UniversalClient, config Config) (*Guard, error) {
	if client == nil || config.Namespace == "" || len(config.Namespace) > 100 ||
		strings.ContainsAny(config.Namespace, "{} \t\r\n") ||
		config.EndpointHost == "" ||
		config.InFlight < 1 ||
		config.InFlight > 1000 ||
		config.RPM < 1 ||
		config.RPM > 10000 ||
		config.RPS < 1 ||
		config.RPS > 10000 ||
		config.WaitBudgetDuration <= 0 ||
		config.WaitBudgetDuration > 65*time.Second ||
		config.TPM < 1 ||
		config.TPM > 1000000000 ||
		!config.TokenBoundVerified ||
		config.TokenOverhead < 0 ||
		config.TokenOverhead > 1000000 {
		return nil, fmt.Errorf("%w: missing verified budget or token contract", ErrClosed)
	}
	raw, _ := json.Marshal(config)
	sum := sha256.Sum256(raw)
	return &Guard{
		client:      client,
		config:      config,
		fingerprint: hex.EncodeToString(sum[:]),
		prefix:      "feishu:safety:{" + config.Namespace + "}:",
	}, nil
}

// Fingerprint identifies the complete local budget and token-bound policy.
func (g *Guard) Fingerprint() string { return g.fingerprint }

// CloseLocal stops this process during a fenced graceful departure without
// clearing or creating a deployment-global trip latch.
func (g *Guard) CloseLocal() { g.localClosed.Store(true) }

// PermitKey is exposed for the controller to issue short-lived permissions ONLY
// after validating the complete expected worker inventory and metric freshness.
func (g *Guard) PermitKey() string      { return g.prefix + "permit" }
func (g *Guard) key(name string) string { return g.prefix + name }

var checkScript = redis.NewScript(`
if redis.call('EXISTS',KEYS[2]) == 1 then return redis.error_reply('SAFETY_TRIPPED') end
if redis.call('GET',KEYS[1]) ~= ARGV[1] or redis.call('PTTL',KEYS[1]) <= 0 then
 return redis.error_reply('SAFETY_NO_PERMIT')
end
return 1`)

// Check requires a live matching controller permit and an absent stop latch.
func (g *Guard) Check(ctx context.Context) error {
	if g == nil || g.localClosed.Load() {
		return ErrClosed
	}
	if err := checkScript.Run(ctx, g.client, []string{g.PermitKey(), g.key("trip")}, g.fingerprint).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrClosed, err)
	}
	return nil
}

// Trip closes this process immediately, persists a latch, removes the permit and
// records a bounded, content-free reason. It never clears itself automatically.
func (g *Guard) Trip(ctx context.Context, reason string) error {
	return g.TripUsing(ctx, g.client, reason)
}

// TripUsing permits the controller to persist the same latch through its
// isolated transport when the business pool is unavailable. The caller must
// supply a client for the same Redis endpoint and DB; no namespace is replaced.
// The ordinary Trip path and budget client remain unchanged.
func (g *Guard) TripUsing(ctx context.Context, client redis.UniversalClient, reason string) error {
	return g.tripUsingEvidence(ctx, client, reason, "")
}

func (g *Guard) tripUsingEvidence(ctx context.Context, client redis.UniversalClient, reason, evidence string) error {
	g.localClosed.Store(true)
	if client == nil {
		return ErrClosed
	}
	if len(reason) > 128 ||
		strings.IndexFunc(
			reason,
			func(r rune) bool { return r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') },
		) >= 0 {
		reason = "unspecified_safety_stop"
	}
	return redis.NewScript(`
local first=redis.call('SET',KEYS[1],ARGV[1],'NX')
redis.call('DEL',KEYS[2])
if first then
 redis.call('XADD',KEYS[3],'MAXLEN','~',1000,'*','event','trip','reason',ARGV[1],'http_evidence',ARGV[2])
end
return 1`).
		// Stop must not depend on an EVALSHA/NOSCRIPT round trip. A stalled
		// cache probe could exhaust the deadline before the latch was written.
		Eval(ctx, client, []string{g.key("trip"), g.PermitKey(), g.key("audit")}, reason, evidence).
		Err()
}

// Snapshot contains no API credentials or document contents. tpm_used is the
// conservative LOCAL reserved upper bound, never the gateway's observed usage.
func (g *Guard) Snapshot(ctx context.Context) (map[string]interface{}, error) {
	values, err := redis.NewScript(`
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[4],'-inf',now-60000)
local entries=redis.call('ZRANGE',KEYS[4],0,-1)
local tokens=0
for _,v in ipairs(entries) do tokens=tokens+tonumber(string.match(v,':(%d+)$')) end
return {redis.call('GET',KEYS[1]) or '',redis.call('PTTL',KEYS[1]),
 redis.call('GET',KEYS[2]) or '',redis.call('HLEN',KEYS[3]),
 #entries,tokens,redis.call('ZCOUNT',KEYS[4],now-1000,'+inf')}`).
		Run(ctx, g.client, []string{g.PermitKey(), g.key("trip"), g.key("inflight"), g.key("window")}).
		Slice()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"fingerprint":                   g.fingerprint,
		"permit_fingerprint":            values[0],
		"permit_ttl_ms":                 values[1],
		"trip":                          values[2],
		"inflight":                      values[3],
		"window_entries":                values[4],
		"rpm_used":                      values[4],
		"tpm_used":                      values[5],
		"rps_used":                      values[6],
		"token_usage_basis":             "verified_utf8_upper_bound",
		"upstream_observed_token_usage": "UNKNOWN",
	}, nil
}

var acquireScript = redis.NewScript(`
if redis.call('EXISTS',KEYS[2]) == 1 then return redis.error_reply('SAFETY_TRIPPED') end
if redis.call('GET',KEYS[1]) ~= ARGV[1] or redis.call('PTTL',KEYS[1]) <= 0 then
 return redis.error_reply('SAFETY_NO_PERMIT')
end
local t=redis.call('TIME'); local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[4],'-inf',now-60000)
local entries=redis.call('ZRANGE',KEYS[4],0,-1); local tokens=0
for _,v in ipairs(entries) do tokens=tokens+tonumber(string.match(v,':(%d+)$')) end
if redis.call('HLEN',KEYS[3]) >= tonumber(ARGV[2]) then return redis.error_reply('SAFETY_INFLIGHT_BUDGET') end
if redis.call('ZCOUNT',KEYS[4],now-1000,'+inf') >= tonumber(ARGV[7]) then
 return redis.error_reply('SAFETY_RPS_BUDGET')
end
if #entries >= tonumber(ARGV[3]) or tokens+tonumber(ARGV[4]) > tonumber(ARGV[5]) then
 return redis.error_reply('SAFETY_WINDOW_BUDGET')
end
redis.call('HSET',KEYS[3],ARGV[6],now)
redis.call('ZADD',KEYS[4],now,ARGV[6]..':'..ARGV[4]); return 1`)

func (g *Guard) tryBegin(ctx context.Context, tokens int) (func(), error) {
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	err := acquireScript.Run(ctx, g.client,
		[]string{g.PermitKey(), g.key("trip"), g.key("inflight"), g.key("window")},
		g.fingerprint, g.config.InFlight, g.config.RPM, tokens, g.config.TPM, id, g.config.RPS).
		Err()
	if err != nil {
		if strings.Contains(err.Error(), "_BUDGET") {
			return nil, ErrBudgetBusy
		}
		return nil, fmt.Errorf("%w: %v", ErrClosed, err)
	}
	// No automatic expiry: a crashed worker retains its slot until an operator
	// proves it drained. Reclaiming a lease while a remote request lives is unsafe.
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = g.client.HDel(releaseCtx, g.key("inflight"), id).Err()
	}, nil
}

func (g *Guard) begin(ctx context.Context, tokens int) (func(), error) {
	if tokens > g.config.TPM {
		return nil, fmt.Errorf("%w: single request exceeds token allocation", ErrClosed)
	}
	deadline := time.NewTimer(g.config.WaitBudgetDuration)
	defer deadline.Stop()
	for {
		release, err := g.tryBegin(ctx, tokens)
		if !errors.Is(err, ErrBudgetBusy) {
			return release, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			// Known backpressure is different from an unconfirmed Redis/permit
			// state. Recheck the gate before returning a retryable local refusal.
			if err := g.Check(ctx); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: budget wait expired", ErrBudgetBusy)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type (
	scopeKey struct{}
	// Scope identifies a legacy Feishu background execution.
	Scope struct{ SourceID string }
)

// WithScope marks background execution for the independent outbound gate.
func WithScope(ctx context.Context, sourceID string) context.Context {
	return context.WithValue(ctx, scopeKey{}, Scope{SourceID: sourceID})
}

// Scoped reports whether this execution requires the Feishu safety gate.
func Scoped(ctx context.Context) bool { _, ok := ctx.Value(scopeKey{}).(Scope); return ok }

var installed atomic.Pointer[Guard]

// InstallDefault installs the process-wide guard used by marked transports.
func InstallDefault(g *Guard) { installed.Store(g) }

// Default returns the installed guard, or nil when none was configured.
func Default() *Guard { return installed.Load() }

// Check enforces the default gate only for marked background executions.
func Check(ctx context.Context) error {
	if !Scoped(ctx) {
		return nil
	}
	return Default().Check(ctx)
}
