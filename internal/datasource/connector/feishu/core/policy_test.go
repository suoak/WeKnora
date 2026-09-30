package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLimiterAndTokenScopeKeysAreSeparated(t *testing.T) {
	appOld := appScopeKey("https://open.feishu.cn", "app-a")
	appNew := appScopeKey("https://open.feishu.cn/", "app-a")
	if appOld != appNew {
		t.Fatal("trailing slash must not split one app limiter scope")
	}
	if tokenScopeKey("https://open.feishu.cn", "app-a", "old") == tokenScopeKey("https://open.feishu.cn", "app-a", "new") {
		t.Fatal("rotated secrets must use distinct token caches")
	}
	if appOld == tokenScopeKey("https://open.feishu.cn", "app-a", "old") {
		t.Fatal("limiter and token cache keys must not be identical")
	}
}

func TestRetryBackoffIsDeterministicWhenInjected(t *testing.T) {
	p := retryPolicy{
		initial: time.Second,
		maximum: 30 * time.Second,
		jitter:  func(base time.Duration) time.Duration { return base / 4 },
	}
	wants := []time.Duration{1250 * time.Millisecond, 2500 * time.Millisecond, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
	for attempt, want := range wants {
		if got := p.delay(attempt, 0); got != want {
			t.Fatalf("delay(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestConcurrencyLimit(t *testing.T) {
	p := &appPolicy{
		limiter:      rate.NewLimiter(rate.Inf, 1),
		workflowGate: make(chan struct{}, 2),
		requestGate:  make(chan struct{}, 2),
		now:          time.Now,
		sleep:        sleepCtx,
	}
	var active atomic.Int64
	var maximum atomic.Int64
	releaseAll := make(chan struct{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := p.acquireWorkflow(context.Background())
			if err != nil {
				t.Errorf("acquireWorkflow() error = %v", err)
				return
			}
			n := active.Add(1)
			for {
				old := maximum.Load()
				if n <= old || maximum.CompareAndSwap(old, n) {
					break
				}
			}
			<-releaseAll
			active.Add(-1)
			release()
		}()
	}
	deadline := time.Now().Add(time.Second)
	for maximum.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(releaseAll)
	wg.Wait()
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum active workflows = %d, want 2", got)
	}
}

func TestPolicyWaitHonorsContextCancellation(t *testing.T) {
	p := &appPolicy{
		limiter:      rate.NewLimiter(rate.Every(time.Hour), 1),
		workflowGate: make(chan struct{}, 1),
		requestGate:  make(chan struct{}, 1),
		now:          time.Now,
		sleep:        sleepCtx,
	}
	p.extendCooldown(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.beforeRequest(ctx); err != context.Canceled {
		t.Fatalf("beforeRequest() error = %v, want context.Canceled", err)
	}
}
