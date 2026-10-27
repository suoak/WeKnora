package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestSourceHeaderDisconnectUsesExistingBoundedRetry(t *testing.T) {
	for _, recoverAfterFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "recovered"}[recoverAfterFirst], func(t *testing.T) {
			store := miniredis.RunT(t)
			rc := redis.NewClient(&redis.Options{Addr: store.Addr(), MaxRetries: -1})
			t.Cleanup(func() { _ = rc.Close(); feishusafety.InstallDefault(nil) })
			guard, err := feishusafety.New(
				rc,
				feishusafety.Config{
					Namespace:          "retry-test-" + uuid.NewString(),
					EndpointHost:       "gateway.invalid",
					InFlight:           1,
					RPM:                10,
					RPS:                2,
					TPM:                10000,
					WaitBudgetDuration: time.Second,
					TokenBoundVerified: true,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := rc.Set(context.Background(), guard.PermitKey(), guard.Fingerprint(), 10*time.Second).Err(); err != nil {
				t.Fatal(err)
			}
			feishusafety.InstallDefault(guard)
			cfg := &Config{BaseURL: "https://source.invalid", AppID: "synthetic-app"}
			client := fastRetryClient(cfg, 2)
			attempts := 0
			client.httpClient.Transport = feishusafety.Transport{
				Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					attempts++
					if !recoverAfterFirst || attempts == 1 {
						return nil, io.EOF
					}
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
					}, nil
				}),
			}
			ctx := WithUserIdentity(
				feishusafety.WithScope(context.Background(), "synthetic-source"),
				func(context.Context) (string, error) { return "synthetic-personal-token", nil },
			)
			err = client.DoRequest(ctx, http.MethodGet, "/metadata", nil, nil)
			if recoverAfterFirst {
				if err != nil || attempts != 2 {
					t.Fatalf("recover: attempts=%d error=%v", attempts, err)
				}
			} else if !errors.Is(err, ErrTransient) || attempts != 3 {
				t.Fatalf("bounded exhaustion: attempts=%d error=%v", attempts, err)
			}
			if guard.Check(context.Background()) != nil {
				t.Fatal("single-source network failure disabled independent sources")
			}
		})
	}
}
