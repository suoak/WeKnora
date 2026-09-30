package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// retryTestServer builds a server that always answers the auth-token call and
// routes the given target path to h, so tests can drive DoRequest's retry loop.
func retryTestServer(target string, h http.HandlerFunc) (*httptest.Server, *Config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, TokenResponse{
			ApiResponse:       ApiResponse{Code: 0},
			TenantAccessToken: "fake-token",
			Expire:            7200,
		})
	})
	mux.HandleFunc(target, h)
	ts := httptest.NewServer(mux)
	return ts, &Config{AppID: "a", AppSecret: "b", BaseURL: ts.URL}
}

func TestDoRequest_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// "0" is coerced to a short delay inside the client so the test stays fast.
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"code":99991400,"msg":"rate limited"}`)
			return
		}
		writeJSON(w, ApiResponse{Code: 0})
	})
	defer ts.Close()

	c := NewClient(cfg)
	var resp ApiResponse
	if err := c.DoRequest(context.Background(), http.MethodGet, "/target", nil, &resp); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want >= 2 (should retry after 429)", attempts)
	}
}

func TestDoRequest_429ExhaustsRetries(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"code":99991400,"msg":"rate limited"}`)
	})
	defer ts.Close()

	c := NewClient(cfg)
	err := c.DoRequest(context.Background(), http.MethodGet, "/target", nil, nil)
	if err == nil {
		t.Fatal("expected error when 429s exceed the retry budget")
	}
	if attempts != defaultFeishuRetryMax+1 {
		t.Errorf("attempts = %d, want %d", attempts, defaultFeishuRetryMax+1)
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}

func TestDoRequest_5xxRetriesToConfiguredLimit(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":1,"msg":"internal error"}`)
	})
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := NewClient(cfg)
	if err := c.DoRequest(ctx, http.MethodGet, "/target", nil, nil); err == nil {
		t.Fatal("expected error after 5xx exhaustion")
	}
	if attempts != defaultFeishuRetryMax+1 {
		t.Errorf("attempts = %d, want %d", attempts, defaultFeishuRetryMax+1)
	}
}

func fastRetryClient(cfg *Config, maxRetries int) *Client {
	c := NewClient(cfg)
	c.retry = retryPolicy{
		maxRetries: maxRetries,
		initial:    time.Millisecond,
		maximum:    5 * time.Millisecond,
		sleep:      sleepCtx,
		now:        time.Now,
		jitter:     func(time.Duration) time.Duration { return 0 },
	}
	c.policy = &appPolicy{
		limiter:      rate.NewLimiter(rate.Inf, 1),
		workflowGate: make(chan struct{}, 2),
		requestGate:  make(chan struct{}, 2),
		now:          time.Now,
		sleep:        sleepCtx,
	}
	return c
}

func TestRetryOnApplicationRateLimit(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			writeJSON(w, ApiResponse{Code: 99991400, Msg: "too many requests"})
			return
		}
		writeJSON(w, ApiResponse{Code: 0})
	})
	defer ts.Close()

	c := fastRetryClient(cfg, 4)
	var response ApiResponse
	if err := c.DoRequest(context.Background(), http.MethodGet, "/target", nil, &response); err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestNoRetryOnPermissionError(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		writeJSON(w, ApiResponse{Code: 99991672, Msg: "permission denied"})
	})
	defer ts.Close()

	err := fastRetryClient(cfg, 4).DoRequest(context.Background(), http.MethodGet, "/target", nil, nil)
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("error = %v, want ErrPermission", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestNoRetryOnAuthError(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		writeJSON(w, ApiResponse{Code: 99991663, Msg: "invalid access token"})
	})
	defer ts.Close()

	err := fastRetryClient(cfg, 4).DoRequest(context.Background(), http.MethodGet, "/target", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("error = %v, want ErrAuth", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryOnTransientServerError(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, ApiResponse{Code: 1, Msg: "temporary"})
			return
		}
		writeJSON(w, ApiResponse{Code: 0})
	})
	defer ts.Close()

	if err := fastRetryClient(cfg, 4).DoRequest(context.Background(), http.MethodGet, "/target", nil, nil); err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestSharedCooldown(t *testing.T) {
	var targetCalls atomic.Int64
	firstResponse := make(chan struct{})
	var once sync.Once
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		n := targetCalls.Add(1)
		if n == 1 {
			once.Do(func() { close(firstResponse) })
			writeJSON(w, ApiResponse{Code: 99991400, Msg: "too many requests"})
			return
		}
		writeJSON(w, ApiResponse{Code: 0})
	})
	defer ts.Close()

	c := fastRetryClient(cfg, 2)
	c.retry.initial = 80 * time.Millisecond
	c.retry.maximum = 80 * time.Millisecond
	errCh := make(chan error, 2)
	go func() { errCh <- c.DoRequest(context.Background(), http.MethodGet, "/target", nil, nil) }()
	<-firstResponse
	deadline := time.Now().Add(time.Second)
	for {
		c.policy.cooldownMu.Lock()
		active := c.policy.cooldownUntil.After(time.Now())
		c.policy.cooldownMu.Unlock()
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rate-limited request did not install shared cooldown")
		}
		time.Sleep(time.Millisecond)
	}
	go func() { errCh <- c.DoRequest(context.Background(), http.MethodGet, "/target", nil, nil) }()
	time.Sleep(25 * time.Millisecond)
	if got := targetCalls.Load(); got != 1 {
		t.Fatalf("target calls during cooldown = %d, want 1", got)
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("request error = %v", err)
		}
	}
}

func TestConcurrentTokenRefreshSingleflight(t *testing.T) {
	var tokenCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		time.Sleep(10 * time.Millisecond)
		writeJSON(w, TokenResponse{ApiResponse: ApiResponse{Code: 0}, TenantAccessToken: "shared", Expire: 7200})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	c := fastRetryClient(&Config{AppID: "singleflight", AppSecret: "secret", BaseURL: ts.URL}, 1)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if token, err := c.GetTenantAccessToken(context.Background()); err != nil || token != "shared" {
				t.Errorf("GetTenantAccessToken() = %q, %v", token, err)
			}
		}()
	}
	wg.Wait()
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token refresh calls = %d, want 1", got)
	}
}

func TestDoRequest_4xxNotRetried(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/target", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":1,"msg":"bad request"}`)
	})
	defer ts.Close()

	c := NewClient(cfg)
	if err := c.DoRequest(context.Background(), http.MethodGet, "/target", nil, nil); err == nil {
		t.Fatal("expected error on 400")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (non-429/5xx 4xx must not retry)", attempts)
	}
}

func TestDownloadRawBytes_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/dl", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("payload-bytes"))
	})
	defer ts.Close()

	c := NewClient(cfg)
	data, err := c.downloadRawBytes(context.Background(), "/dl")
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if string(data) != "payload-bytes" {
		t.Errorf("data = %q, want %q", string(data), "payload-bytes")
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want >= 2 (should retry download after 429)", attempts)
	}
}

func TestDownloadRawBytes_4xxNotRetried(t *testing.T) {
	var attempts int
	ts, cfg := retryTestServer("/dl", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusForbidden)
	})
	defer ts.Close()

	c := NewClient(cfg)
	if _, err := c.downloadRawBytes(context.Background(), "/dl"); err == nil {
		t.Fatal("expected error on 403")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (403 must not retry)", attempts)
	}
}

func TestParseRetryAfter(t *testing.T) {
	fallback := 5 * time.Second
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", fallback},
		{"0", 100 * time.Millisecond},
		{"-1", 100 * time.Millisecond}, // negative coerced to a short delay
		{"3", 3 * time.Second},
		{"abc", fallback}, // unparseable
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.header, fallback); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}
