package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultFeishuRPS             = 2.0
	defaultFeishuBurst           = 2
	defaultFeishuSyncConcurrency = 2
	defaultFeishuHTTPConcurrency = 2
	defaultFeishuRetryMax        = 6
	defaultFeishuInitialBackoff  = time.Second
	defaultFeishuMaximumBackoff  = 30 * time.Second
)

type sleepFunc func(context.Context, time.Duration) error

type retryPolicy struct {
	maxRetries int
	initial    time.Duration
	maximum    time.Duration
	sleep      sleepFunc
	now        func() time.Time
	jitter     func(time.Duration) time.Duration
}

func defaultRetryPolicy() retryPolicy {
	initialBackoffMS := envPositiveInt(
		"FEISHU_API_RETRY_INITIAL_BACKOFF_MS",
		int(defaultFeishuInitialBackoff/time.Millisecond),
	)
	maximumBackoffMS := envPositiveInt(
		"FEISHU_API_RETRY_MAX_BACKOFF_MS",
		int(defaultFeishuMaximumBackoff/time.Millisecond),
	)
	return retryPolicy{
		maxRetries: envNonNegativeInt("FEISHU_API_RETRY_MAX", defaultFeishuRetryMax),
		initial:    time.Duration(initialBackoffMS) * time.Millisecond,
		maximum:    time.Duration(maximumBackoffMS) * time.Millisecond,
		sleep:      sleepCtx,
		now:        time.Now,
		jitter: func(base time.Duration) time.Duration {
			window := base / 4
			if window <= 0 {
				return 0
			}
			return time.Duration(rand.Int63n(int64(window) + 1))
		},
	}
}

func (p retryPolicy) delay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > p.maximum {
			return p.maximum
		}
		return retryAfter
	}
	d := p.initial
	for i := 0; i < attempt && d < p.maximum; i++ {
		if d > p.maximum/2 {
			d = p.maximum
			break
		}
		d *= 2
	}
	if d > p.maximum {
		d = p.maximum
	}
	if p.jitter != nil && d < p.maximum {
		d += p.jitter(d)
		if d > p.maximum {
			d = p.maximum
		}
	}
	return d
}

// appPolicy is shared by every client using one Feishu app. Its key excludes
// app_secret deliberately: rotating the secret must not create a second rate
// budget for the same upstream application.
type appPolicy struct {
	limiter      *rate.Limiter
	workflowGate chan struct{}
	requestGate  chan struct{}

	cooldownMu    sync.Mutex
	cooldownUntil time.Time
	now           func() time.Time
	sleep         sleepFunc

	retryCount atomic.Int64
}

var appPolicyRegistry sync.Map

func appScopeKey(baseURL, appID string) string {
	return hashScope(strings.TrimRight(baseURL, "/") + "\x00" + appID)
}

func tokenScopeKey(baseURL, appID, appSecret string) string {
	return hashScope(strings.TrimRight(baseURL, "/") + "\x00" + appID + "\x00" + appSecret)
}

func hashScope(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func sharedAppPolicy(baseURL, appID string, rp retryPolicy) *appPolicy {
	key := appScopeKey(baseURL, appID)
	if existing, ok := appPolicyRegistry.Load(key); ok {
		return existing.(*appPolicy)
	}
	rateLimit := envPositiveFloat("FEISHU_API_RATE_LIMIT_RPS", defaultFeishuRPS)
	burst := envPositiveInt("FEISHU_API_RATE_LIMIT_BURST", defaultFeishuBurst)
	p := &appPolicy{
		limiter:      rate.NewLimiter(rate.Limit(rateLimit), burst),
		workflowGate: make(chan struct{}, envPositiveInt("FEISHU_SYNC_CONCURRENCY", defaultFeishuSyncConcurrency)),
		requestGate:  make(chan struct{}, envPositiveInt("FEISHU_API_MAX_INFLIGHT", defaultFeishuHTTPConcurrency)),
		now:          rp.now,
		sleep:        rp.sleep,
	}
	actual, _ := appPolicyRegistry.LoadOrStore(key, p)
	return actual.(*appPolicy)
}

func (p *appPolicy) acquireWorkflow(ctx context.Context) (func(), error) {
	select {
	case p.workflowGate <- struct{}{}:
		return func() { <-p.workflowGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *appPolicy) beforeRequest(ctx context.Context) (func(), error) {
	if err := p.waitCooldown(ctx); err != nil {
		return nil, err
	}
	if err := p.limiter.Wait(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			return nil, fmt.Errorf("%w: rate limiter wait: %v", context.DeadlineExceeded, err)
		}
		return nil, err
	}
	// A different request may have installed a cooldown while this caller was
	// waiting for a rate token. Re-check before entering the in-flight gate.
	if err := p.waitCooldown(ctx); err != nil {
		return nil, err
	}
	select {
	case p.requestGate <- struct{}{}:
		return func() { <-p.requestGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *appPolicy) waitCooldown(ctx context.Context) error {
	for {
		p.cooldownMu.Lock()
		wait := p.cooldownUntil.Sub(p.now())
		p.cooldownMu.Unlock()
		if wait <= 0 {
			return nil
		}
		if err := p.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (p *appPolicy) extendCooldown(delay time.Duration) {
	if delay <= 0 {
		return
	}
	until := p.now().Add(delay)
	p.cooldownMu.Lock()
	if until.After(p.cooldownUntil) {
		p.cooldownUntil = until
	}
	p.cooldownMu.Unlock()
}

func envPositiveInt(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envNonNegativeInt(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

func envPositiveFloat(name string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
