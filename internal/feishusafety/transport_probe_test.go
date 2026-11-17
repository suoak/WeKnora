package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEmbeddingTransportProbeFailurePreservesHealthySources(t *testing.T) {
	for _, failureAt := range []int32{2, 3} {
		for _, lostPermit := range []bool{false, true} {
			t.Run(fmt.Sprintf("probe=%d/lost_permit=%t", failureAt, lostPermit), func(t *testing.T) {
				g, client := fixture(t, Config{})
				InstallDefault(g)
				var calls atomic.Int32
				g.SetApprovedSourceValidator(func(ctx context.Context, source, _ string) error {
					if source == "affected-source" && calls.Add(1) >= failureAt {
						if lostPermit {
							if err := client.Del(ctx, g.PermitKey()).Err(); err != nil {
								return err
							}
						}
						return errors.New("source probe unavailable")
					}
					return nil
				})
				ctx := WithScope(context.Background(), "affected-source")
				req, err := http.NewRequestWithContext(ctx, http.MethodPost,
					"https://gateway.invalid/embeddings", strings.NewReader(`{"input":["verified input"]}`))
				if err != nil {
					t.Fatal(err)
				}
				response, err := (Transport{Base: forbiddenBudgetWire{t}, Embedding: true}).RoundTrip(req)
				if response != nil {
					t.Fatal("denied admission returned a response")
				}
				if lostPermit {
					if !errors.Is(err, ErrClosed) {
						t.Fatalf("lost controller permitted work: %v", err)
					}
				} else if !errors.Is(err, ErrSourceValidationUnavailable) {
					t.Fatalf("lost retryable source-probe category: %v", err)
				}
				if tripped := client.Exists(ctx, g.key("trip")).Val() != 0; tripped != lostPermit {
					t.Fatalf("wrong global stop: lostPermit=%v err=%v", lostPermit, err)
				}
				if client.HLen(ctx, g.key("inflight")).Val() != 0 || client.ZCard(ctx, g.key("window")).Val() != 0 {
					t.Fatal("denied admission consumed a slot or token budget")
				}
				if !lostPermit && g.Check(WithScope(context.Background(), "healthy-source")) != nil {
					t.Fatal("independent source cannot continue")
				}
			})
		}
	}
}
