package feishusafety

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func fixture(t *testing.T, cfg Config) (*Guard, *redis.Client) {
	t.Helper()
	addr := os.Getenv("FEISHU_SAFETY_REDIS_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	} else {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || host != "127.0.0.1" {
			t.Fatal("isolated Redis must be explicit loopback address")
		}
	}
	client := redis.NewClient(
		&redis.Options{
			Addr:         addr,
			MaxRetries:   -1,
			DialTimeout:  200 * time.Millisecond,
			ReadTimeout:  200 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
		},
	)
	t.Cleanup(func() { _ = client.Close() })
	cfg.Namespace = "test-" + uuid.NewString()
	if cfg.EndpointHost == "" {
		cfg.EndpointHost = "gateway.invalid"
	}
	if cfg.InFlight == 0 {
		cfg.InFlight = 100
	}
	if cfg.RPM == 0 {
		cfg.RPM = 100
	}
	if cfg.RPS == 0 {
		cfg.RPS = 100
	}
	if cfg.WaitBudgetDuration == 0 {
		cfg.WaitBudgetDuration = 65 * time.Second
	}
	if cfg.TPM == 0 {
		cfg.TPM = 10000
	}
	cfg.TokenBoundVerified = true
	g, err := New(client, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Set(context.Background(), g.PermitKey(), g.Fingerprint(), 10*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		InstallDefault(nil)
		keys, _ := client.Keys(context.Background(), g.prefix+"*").Result()
		if len(keys) > 0 {
			client.Del(context.Background(), keys...)
		}
	})
	return g, client
}

func TestSafetyAtomicBudgets(t *testing.T) {
	for _, kind := range []string{"inflight", "rpm", "tpm"} {
		t.Run(kind, func(t *testing.T) {
			cfg := Config{}
			switch kind {
			case "inflight":
				cfg.InFlight = 3
			case "rpm":
				cfg.RPM = 3
			case "tpm":
				cfg.TPM = 3
			}
			g, client := fixture(t, cfg)
			other, err := New(client, g.config)
			if err != nil {
				t.Fatal(err)
			}
			var successes atomic.Int32
			var wg sync.WaitGroup
			var mu sync.Mutex
			var releases []func()
			for n := range 40 {
				worker := g
				if n%2 == 1 {
					worker = other
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					release, err := worker.tryBegin(context.Background(), 1)
					if err == nil {
						successes.Add(1)
						mu.Lock()
						releases = append(releases, release)
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			if successes.Load() != 3 {
				t.Fatalf("atomic budget admitted %d, want 3", successes.Load())
			}
			for _, release := range releases {
				release()
			}
		})
	}
}

func TestSafetyStopsDuringResponseRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()
	g, client := fixture(t, Config{})
	InstallDefault(g)
	req, _ := http.NewRequestWithContext(
		WithScope(context.Background(), uuid.NewString()),
		http.MethodGet,
		server.URL,
		nil,
	)
	resp, err := (&http.Client{Transport: Transport{}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, resp.Body); done <- err }()
	client.Del(context.Background(), g.PermitKey())
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("response unexpectedly completed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response read not canceled")
	}
}

func TestSafetyMissingStateAndMismatch(t *testing.T) {
	g, client := fixture(t, Config{})
	ctx := context.Background()
	client.Set(ctx, g.PermitKey(), "another-worker-config", time.Second)
	if g.Check(ctx) == nil {
		t.Fatal("fingerprint mismatch accepted")
	}
	client.Del(ctx, g.PermitKey())
	if g.Check(ctx) == nil {
		t.Fatal("missing controller accepted")
	}
	client.Set(ctx, g.PermitKey(), g.Fingerprint(), 20*time.Millisecond)
	time.Sleep(35 * time.Millisecond)
	// miniredis only advances TTL explicitly; remove permit to simulate expiry.
	client.Del(ctx, g.PermitKey())
	if g.Check(ctx) == nil {
		t.Fatal("expired controller accepted")
	}
	if _, err := New(client, Config{Namespace: "unknown", EndpointHost: "gateway.invalid", InFlight: 1, RPM: 1, TPM: 1}); err == nil {
		t.Fatal("unknown token contract accepted")
	}
}

func TestSafetyHTTPFaultAndOnlineIsolation(t *testing.T) {
	for _, status := range []int{500, 503, 408, 402} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }),
			)
			defer server.Close()
			g, _ := fixture(t, Config{EndpointHost: strings.TrimPrefix(server.URL, "http://")})
			InstallDefault(g)
			client := &http.Client{Transport: Transport{Base: http.DefaultTransport, Embedding: true}}
			req, _ := http.NewRequestWithContext(
				WithScope(context.Background(), uuid.NewString()),
				http.MethodPost,
				server.URL,
				strings.NewReader(`{"input":["你好"]}`),
			)
			if _, err := client.Do(req); err == nil {
				t.Fatal("fault accepted")
			}
			if g.Check(context.Background()) == nil {
				t.Fatal("fault did not trip")
			}
			req, _ = http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"input":["online"]}`))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("online affected", err)
			}
			resp.Body.Close()
		})
	}
}

func TestSafetyControllerLossCancelsActiveHTTP(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		close(canceled)
	}))
	defer server.Close()
	g, client := fixture(t, Config{EndpointHost: strings.TrimPrefix(server.URL, "http://")})
	InstallDefault(g)
	req, _ := http.NewRequestWithContext(
		WithScope(context.Background(), uuid.NewString()),
		http.MethodPost,
		server.URL,
		strings.NewReader(`{"input":["test"]}`),
	)
	finished := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Transport: Transport{Embedding: true}}).Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		finished <- err
	}()
	<-entered
	client.Del(context.Background(), g.PermitKey())
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active request not canceled")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("server did not observe cancellation")
	}
}

func TestSafetyRedisLossAndEmergencyStop(t *testing.T) {
	for _, loss := range []string{"redis", "trip"} {
		t.Run(loss, func(t *testing.T) {
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-r.Context().Done():
				case <-time.After(3 * time.Second):
				}
			}))
			defer server.Close()
			g, client := fixture(t, Config{})
			InstallDefault(g)
			req, _ := http.NewRequestWithContext(
				WithScope(context.Background(), uuid.NewString()),
				http.MethodGet,
				server.URL,
				nil,
			)
			finished := make(chan error, 1)
			go func() {
				resp, err := (&http.Client{Transport: Transport{}}).Do(req)
				if resp != nil {
					resp.Body.Close()
				}
				finished <- err
			}()
			<-entered
			if loss == "redis" {
				client.Close()
			} else {
				if err := g.Trip(context.Background(), "isolated_test_stop"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("expected cancellation")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stop ineffective")
			}
		})
	}
}

func TestSafetyBodyAndTokenAccounting(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[]}`) }),
	)
	defer server.Close()
	g, _ := fixture(t, Config{EndpointHost: strings.TrimPrefix(server.URL, "http://"), TPM: 8, TokenOverhead: 1})
	InstallDefault(g)
	c := &http.Client{Transport: Transport{Embedding: true}}
	req, _ := http.NewRequestWithContext(
		WithScope(context.Background(), uuid.NewString()),
		http.MethodPost,
		server.URL,
		strings.NewReader(`{"input":["你好"]}`),
	)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	snapshot, err := g.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot["inflight"] != int64(0) {
		t.Fatal(snapshot)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(
		WithScope(waitCtx, uuid.NewString()),
		http.MethodPost,
		server.URL,
		strings.NewReader(`{"input":["ab"]}`),
	)
	if _, err = c.Do(req); err == nil {
		t.Fatal("UTF8 byte bound not charged")
	}
	if g.Check(context.Background()) != nil {
		t.Fatal("normal budget queue cancellation tripped gate")
	}
}

func TestSafetyRPSQueuesAcrossWorkers(t *testing.T) {
	g, client := fixture(t, Config{RPS: 1})
	other, err := New(client, g.config)
	if err != nil {
		t.Fatal(err)
	}
	release, err := g.begin(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	release()
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		release, err := other.begin(context.Background(), 1)
		if err == nil {
			release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("RPS second wire was not queued", err)
	case <-time.After(150 * time.Millisecond):
	}
	// Redis TIME, rather than this process's clock, controls the shared window.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < 900*time.Millisecond {
			t.Fatal("rolling one-second budget not enforced")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RPS wait never released")
	}
}

func TestSafety429RetryBudget(t *testing.T) {
	for _, kind := range []string{"recover", "exhaust", "retry_after_seconds", "retry_after_date", "short_seconds", "short_date", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if (kind == "short_seconds" || kind == "short_date") && n == 2 {
					io.WriteString(w, `{"data":[]}`)
					return
				}
				if kind == "recover" && n == 3 {
					io.WriteString(w, `{"data":[]}`)
					return
				}
				if kind == "retry_after_seconds" {
					w.Header().Set("Retry-After", "31")
				}
				if kind == "retry_after_date" {
					w.Header().Set("Retry-After", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat))
				}
				if kind == "short_seconds" {
					w.Header().Set("Retry-After", "2")
				}
				if kind == "short_date" {
					w.Header().Set("Retry-After", time.Now().Add(5*time.Second).UTC().Format(http.TimeFormat))
				}
				w.WriteHeader(429)
			}))
			defer server.Close()
			g, _ := fixture(t, Config{EndpointHost: strings.TrimPrefix(server.URL, "http://")})
			InstallDefault(g)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var waits []time.Duration
			transport := Transport{
				Embedding:   true,
				retryJitter: func() time.Duration { return 0 },
				retrySleep: func(ctx context.Context, d time.Duration) error {
					waits = append(waits, d)
					if kind == "cancel" {
						cancel()
						return ctx.Err()
					}
					return g.Check(ctx)
				},
			}
			req, _ := http.NewRequestWithContext(
				WithScope(ctx, uuid.NewString()),
				http.MethodPost,
				server.URL,
				strings.NewReader(`{"input":["x"]}`),
			)
			resp, err := (&http.Client{Transport: transport}).Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			if kind == "short_seconds" || kind == "short_date" {
				if err != nil || calls.Load() != 2 || len(waits) != 1 {
					t.Fatal("RetryAfter did not recover", err, calls.Load(), waits)
				}
				if kind == "short_seconds" && waits[0] != 2*time.Second {
					t.Fatal(waits)
				}
				if kind == "short_date" && waits[0] < 3*time.Second {
					t.Fatal(waits)
				}
			} else if kind == "recover" {
				if err != nil || calls.Load() != 3 || len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
					t.Fatal("retry recovery", err, calls.Load(), waits)
				}
				snapshot, _ := g.Snapshot(context.Background())
				if snapshot["rpm_used"] != int64(3) || snapshot["tpm_used"] != int64(3) {
					t.Fatal("retry attempts not fully charged", snapshot)
				}
			} else if kind == "exhaust" {
				if err == nil || calls.Load() != 3 || g.Check(context.Background()) == nil {
					t.Fatal("exhaustion did not latch", err, calls.Load())
				}
			} else {
				if err == nil || calls.Load() != 1 {
					t.Fatal("unexpected premature retry", err, calls.Load())
				}
				if kind != "cancel" && g.Check(context.Background()) == nil {
					t.Fatal("excessive retry-after did not latch")
				}
			}
		})
	}
}

func TestSafetySequentialEmbeddingQueuesWithoutTrip(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{"data":[]}`) },
		),
	)
	defer server.Close()
	g, _ := fixture(t, Config{EndpointHost: strings.TrimPrefix(server.URL, "http://"), RPS: 1, InFlight: 1})
	InstallDefault(g)
	client := &http.Client{Transport: Transport{Embedding: true}}
	start := time.Now()
	for range 2 {
		req, _ := http.NewRequestWithContext(
			WithScope(context.Background(), uuid.NewString()),
			http.MethodPost,
			server.URL,
			strings.NewReader(`{"input":["next-batch"]}`),
		)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if calls.Load() != 2 || time.Since(start) < 900*time.Millisecond || g.Check(context.Background()) != nil {
		t.Fatal("ordinary serial batches tripped or bypassed RPS", calls.Load(), time.Since(start))
	}
}

func TestSafetySource429DoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(429) }),
	)
	defer server.Close()
	g, _ := fixture(t, Config{})
	InstallDefault(g)
	req, _ := http.NewRequestWithContext(
		WithScope(context.Background(), uuid.NewString()),
		http.MethodGet,
		server.URL,
		nil,
	)
	if _, err := (&http.Client{Transport: Transport{}}).Do(req); err == nil || calls.Load() != 1 ||
		g.Check(context.Background()) == nil {
		t.Fatal("source429 did not stop", err, calls.Load())
	}
}
