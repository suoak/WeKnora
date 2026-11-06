package feishusafety

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestEmbeddingReservationProbeFailurePreservesHealthySources(t *testing.T) {
	for _, lostPermit := range []bool{false, true} {
		g, client := fixture(t, Config{})
		InstallDefault(g)
		var calls atomic.Int32
		g.SetApprovedSourceValidator(func(ctx context.Context, source, _ string) error {
			if source == "affected-source" && calls.Add(1) > 1 {
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
		_, cleanup, err := ReserveEmbedding(ctx, []string{"verified input"})
		if cleanup != nil {
			cleanup()
			t.Fatal("denied reservation returned a ticket")
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
			t.Fatal("denied reservation consumed a slot or token budget")
		}
		if !lostPermit && g.Check(WithScope(context.Background(), "healthy-source")) != nil {
			t.Fatal("independent source cannot continue")
		}
	}
}
