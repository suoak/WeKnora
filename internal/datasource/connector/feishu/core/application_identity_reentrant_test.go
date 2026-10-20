package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// The application provider performs a real metadata request through the same
// app limiter. With two outer requests, a provider called while holding an
// in-flight slot exhausts the shared gate and prevents both from finishing.
func TestApplicationIdentityNestedACLRequestsMustNotHoldInflightSlots(t *testing.T) {
	var active, maximum atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	c := NewClient(&Config{AppID: t.Name()})
	c.baseURL, c.httpClient = srv.URL, srv.Client()
	c.policy = &appPolicy{
		limiter:     rate.NewLimiter(rate.Inf, 1),
		requestGate: make(chan struct{}, 2),
		now:         time.Now,
		sleep:       sleepCtx,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var arrivals atomic.Int32
	both := make(chan struct{})
	var once sync.Once
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			calls := 0
			identity := WithApplicationIdentity(ctx, func(call context.Context) (string, error) {
				calls++
				if calls == 2 {
					if arrivals.Add(1) == 2 {
						once.Do(func() { close(both) })
					}
					select {
					case <-both:
					case <-call.Done():
						return "", call.Err()
					}
				}
				probe := WithApplicationIdentity(
					call,
					func(context.Context) (string, error) { return "test-app-token", nil },
				)
				var response map[string]any
				if err := c.DoRequest(probe, http.MethodGet, "/acl", nil, &response); err != nil {
					return "", err
				}
				return "test-app-token", nil
			})
			var response map[string]any
			results <- c.DoRequest(identity, http.MethodGet, "/content", nil, &response)
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Errorf("nested ACL request did not complete: %v", err)
		}
	}
	if maximum.Load() > 2 {
		t.Fatalf("HTTP in-flight limit exceeded: %d", maximum.Load())
	}
}

func TestApplicationIdentityLocalRevocationWhileReacquiringSlot(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sent.Add(1)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	c := NewClient(&Config{AppID: t.Name()})
	c.baseURL, c.httpClient = srv.URL, srv.Client()
	c.policy = &appPolicy{
		limiter:     rate.NewLimiter(rate.Inf, 1),
		requestGate: make(chan struct{}, 2),
		now:         time.Now,
		sleep:       sleepCtx,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ready, resume := make(chan struct{}), make(chan struct{})
	var revoked atomic.Bool
	calls := 0
	identity := WithApplicationIdentityValidation(ctx, func(call context.Context) (string, error) {
		calls++
		if calls == 2 {
			close(ready)
			select {
			case <-resume:
			case <-call.Done():
				return "", call.Err()
			}
		}
		return "test-app-token", nil
	}, func(context.Context) error {
		if revoked.Load() {
			return ErrPermission
		}
		return nil
	})
	result := make(chan error, 1)
	go func() {
		var response map[string]any
		result <- c.DoRequest(identity, http.MethodGet, "/content", nil, &response)
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("provider never reached post-pacing validation")
	}
	// Both slots are free while the provider is active. Occupy them to force
	// another wait, revoke locally, then release one slot.
	for i := 0; i < 2; i++ {
		select {
		case c.policy.requestGate <- struct{}{}:
		default:
			t.Fatal("provider retained an HTTP slot")
		}
	}
	close(resume)
	revoked.Store(true)
	<-c.policy.requestGate
	if err := <-result; !errors.Is(err, ErrPermission) {
		t.Fatalf("local revocation was not enforced: %v", err)
	}
	<-c.policy.requestGate
	if sent.Load() != 0 {
		t.Fatalf("revoked identity sent %d requests", sent.Load())
	}
}
