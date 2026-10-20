package core

import (
	"context"
	"errors"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStabilityResponseBudgets(t *testing.T) {
	for _, tc := range []struct {
		name        string
		binary      bool
		status      int
		size, limit int64
	}{
		{"json_exact", false, 200, 64, 64}, {"json_oversized", false, 200, 65, 64},
		{"json_error", false, 403, 65, 64}, {"binary_exact", true, 200, 64, 64},
		{"binary_oversized", true, 200, 65, 64}, {"binary_error", true, 403, maxFeishuErrorBytes + 1, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			var closed atomic.Bool
			client := NewClient(&Config{AppID: tc.name, BaseURL: "https://open.feishu.cn"})
			client.jsonLimit = tc.limit
			client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: &stabilityBody{Reader: strings.NewReader(strings.Repeat("x", int(tc.size))), closed: &closed}}, nil
			})}
			_, err := client.doRequestBytes(context.Background(), "budget", "GET", "/budget", nil, false, tc.binary, tc.limit)
			oversized := tc.size > tc.limit
			if tc.binary && tc.status != 200 {
				oversized = tc.size > maxFeishuErrorBytes
			}
			if errors.Is(err, ErrResponseTooLarge) != oversized {
				t.Fatalf("classification err=%v oversized=%v", err, oversized)
			}
			if calls.Load() != 1 || !closed.Load() {
				t.Fatal("response retried or not closed")
			}
		})
	}
}

type stabilityBody struct {
	io.Reader
	closed *atomic.Bool
}

func (b *stabilityBody) Close() error { b.closed.Store(true); return nil }

func TestStabilityIdentityRevalidatedAfterCooldown(t *testing.T) {
	for _, mode := range []string{"revoke", "refresh"} {
		t.Run(mode, func(t *testing.T) {
			var wire atomic.Int32
			var current atomic.Value
			current.Store("old")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wire.Add(1)
				if r.Header.Get("Authorization") != "Bearer new" {
					t.Error("stale or tenant token sent")
				}
				_, _ = io.WriteString(w, `{"code":0}`)
			}))
			defer server.Close()
			client := NewClient(&Config{AppID: mode, BaseURL: server.URL})
			gate := client.requestGate("GET", "/target")
			gate.cooldown(60 * time.Millisecond)
			resolved := make(chan struct{}, 1)
			ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) {
				select {
				case resolved <- struct{}{}:
				default:
				}
				value := current.Load().(string)
				if value == "revoked" {
					return "", ErrAuth
				}
				return value, nil
			})
			done := make(chan error, 1)
			go func() { done <- client.DoRequest(ctx, "GET", "/target", nil, nil) }()
			<-resolved
			if mode == "revoke" {
				current.Store("revoked")
			} else {
				current.Store("new")
			}
			err := <-done
			if mode == "revoke" && (!errors.Is(err, ErrAuth) || wire.Load() != 0) {
				t.Fatalf("revoked request sent: %v", err)
			}
			if mode == "refresh" && (err != nil || wire.Load() != 1) {
				t.Fatalf("refresh failed: %v", err)
			}
		})
	}
}

func TestStabilityUserAndTenantShareQuotaWithoutSharingTokens(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	var sent []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")]++
		sent = append(sent, time.Now())
		mu.Unlock()
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()
	clients := []*Client{NewClient(&Config{AppID: "mixed", BaseURL: server.URL}), NewClient(&Config{AppID: "mixed", BaseURL: server.URL}), NewClient(&Config{AppID: "mixed", BaseURL: server.URL})}
	for _, c := range clients {
		c.token.cache = "tenant"
		c.token.expAt = time.Now().Add(time.Hour)
	}
	gate := clients[0].requestGate("GET", "/open-apis/drive/v1/export_tasks/one")
	gate.interval = 20 * time.Millisecond
	clients[0].policy.limiter = rate.NewLimiter(rate.Inf, 1)
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			ctx := context.Background()
			if i < 2 {
				user := []string{"A", "B"}[i]
				ctx = WithUserIdentity(ctx, func(context.Context) (string, error) { return user, nil })
			}
			if err := c.DoRequest(ctx, "GET", "/open-apis/drive/v1/export_tasks/"+[]string{"one", "two", "three"}[i], nil, nil); err != nil {
				t.Error(err)
			}
		}(i, c)
	}
	wg.Wait()
	if seen["Bearer A"] != 1 || seen["Bearer B"] != 1 || seen["Bearer tenant"] != 1 {
		t.Fatalf("wrong identity counts: %v", seen)
	}
	if len(sent) != 3 || sent[2].Sub(sent[0]) < 30*time.Millisecond {
		t.Fatal("document tokens split quota or pacing burst")
	}
}

func TestStabilityHeaderCooldownAndGroups(t *testing.T) {
	now := time.Now()
	h := make(http.Header)
	h.Set("Retry-After", "1")
	h.Set("x-ogw-ratelimit-reset", "2")
	if got := feishuRateLimitWait(h, now); got != 2*time.Second {
		t.Fatalf("dual header=%v", got)
	}
	h.Set("Retry-After", now.Add(3*time.Second).UTC().Format(http.TimeFormat))
	if got := feishuRateLimitWait(h, now); got < 2*time.Second {
		t.Fatalf("date ignored=%v", got)
	}
	a := NewClient(&Config{AppID: "group-a"})
	b := NewClient(&Config{AppID: "group-b"})
	first := a.requestGate("GET", "/open-apis/drive/v1/export_tasks/t1?token=d1")
	if first != a.requestGate("GET", "/open-apis/drive/v1/export_tasks/t2?token=d2") {
		t.Fatal("per document budget")
	}
	if first == a.requestGate("POST", "/open-apis/drive/v1/export_tasks") || first == b.requestGate("GET", "/open-apis/drive/v1/export_tasks/t1") {
		t.Fatal("groups/apps not isolated")
	}
	first.cooldown(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(first.wait(ctx), context.Canceled) {
		t.Fatal("cancel ignored")
	}
	if strings.Contains((&APIError{Message: "secret-canary", Category: ErrorCategoryPermission}).Error(), "secret-canary") {
		t.Fatal("provider message leaked")
	}
}

func TestStabilityBinaryRateEnvelopeExhaustionRetainsCooldown(t *testing.T) {
	for _, status := range []int{200, 400, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("x-ogw-ratelimit-reset", "1")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"code":99991400}`)
			}))
			defer server.Close()
			client := NewClient(&Config{AppID: "exhaust", BaseURL: server.URL})
			client.retry.maxRetries = 0
			ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user-A", nil })
			_, err := client.downloadRawBytes(ctx, "/open-apis/drive/v1/files/doc/download")
			if !errors.Is(err, ErrRateLimited) || calls.Load() != 1 {
				t.Fatalf("classification/calls: %v %d", err, calls.Load())
			}
			gate := client.requestGate("GET", "/open-apis/drive/v1/files/another/download")
			gate.mu.Lock()
			remaining := time.Until(gate.blockedUntil)
			gate.mu.Unlock()
			if remaining < 500*time.Millisecond {
				t.Fatal("final failure lost shared cooldown")
			}
		})
	}
}

func TestStabilityExportPollBudgetIsFinite(t *testing.T) {
	t.Setenv("FEISHU_EXPORT_MAX_POLLS", "2")
	t.Setenv("FEISHU_EXPORT_POLL_INTERVAL", "1ms")
	var creates, polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/drive/v1/export_tasks":
			creates.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"same-ticket"}}`)
		case "/open-apis/drive/v1/export_tasks/same-ticket":
			polls.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"result":{"job_status":2}}}`)
		default:
			t.Error("unexpected export request")
		}
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "poll-budget", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user", nil })
	_, _, err := client.ExportAndDownload(ctx, "doc", "docx")
	if !errors.Is(err, ErrTransient) || creates.Load() != 1 || polls.Load() != 2 {
		t.Fatalf("budget: creates=%d polls=%d err=%v", creates.Load(), polls.Load(), err)
	}
}
