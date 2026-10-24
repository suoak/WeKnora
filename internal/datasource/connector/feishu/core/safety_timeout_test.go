package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestSafetySourceReadTimeoutUsesFiniteRetry(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "recovers"}[recovery], func(t *testing.T) {
			server := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = rdb.Close(); feishusafety.InstallDefault(nil) })
			cfg := feishusafety.DefaultConfig()
			cfg.Namespace = uuid.NewString()
			guard, err := feishusafety.New(rdb, cfg)
			if err != nil {
				t.Fatal(err)
			}
			err = rdb.Set(context.Background(), guard.PermitKey(), guard.Fingerprint(), time.Minute).Err()
			if err != nil {
				t.Fatal(err)
			}
			feishusafety.InstallDefault(guard)
			var calls atomic.Int32
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/other" || (calls.Add(1) > 1 && recovery) {
					_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer httpServer.Close()
			client := NewClient(&Config{AppID: uuid.NewString(), BaseURL: httpServer.URL})
			client.retry.maxRetries = 2
			client.retry.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			client.httpClient = &http.Client{
				Timeout: 100 * time.Millisecond,
				Transport: feishusafety.Transport{
					Base: httpServer.Client().Transport,
				},
			}
			ctx, cancel := context.WithTimeout(
				feishusafety.WithScope(context.Background(), "source-a"), 5*time.Second,
			)
			defer cancel()
			_, err = client.doRequestBytes(
				ctx, "source_read", http.MethodGet, "/slow", nil, false, false, 0,
			)
			if recovery {
				if err != nil || calls.Load() != 2 {
					stats, _ := guard.Snapshot(context.Background())
					t.Fatalf("source did not recover: calls=%d err=%v state=%v", calls.Load(), err, stats)
				}
			} else if !errors.Is(err, ErrTransient) || calls.Load() != 3 {
				stats, _ := guard.Snapshot(context.Background())
				t.Fatalf("retry not finite/typed: calls=%d err=%v state=%v", calls.Load(), err, stats)
			}
			if !recovery && !strings.Contains(err.Error(), "cause=timeout") {
				t.Fatal("source timeout lost its content-free UI diagnosis", err)
			}
			if err = guard.Check(context.Background()); err != nil {
				stats, _ := guard.Snapshot(context.Background())
				t.Fatal("source timeout stopped independent sources", err, stats)
			}
			_, err = client.doRequestBytes(feishusafety.WithScope(context.Background(), "source-b"),
				"other_read", http.MethodGet, "/other", nil, false, false, 0)
			if err != nil {
				t.Fatal("independent source blocked", err)
			}
		})
	}
}
