package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSafetyKnownBudgetWaitDoesNotTrip(t *testing.T) {
	for _, kind := range []string{"inflight", "rps", "rpm", "tpm"} {
		for _, reserved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_reserved_%v", kind, reserved), func(t *testing.T) {
				cfg := Config{WaitBudgetDuration: 10 * time.Millisecond, TPM: 1000}
				cost := 1
				switch kind {
				case "inflight":
					cfg.InFlight = 1
				case "rpm":
					cfg.RPM = 1
				case "rps":
					cfg.RPS = 1
				case "tpm":
					cost = cfg.TPM
				}
				g, client := fixture(t, cfg)
				InstallDefault(g)
				ctx := WithScope(context.Background(), "source-a")
				release, err := g.tryBegin(ctx, cost)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				if kind != "inflight" {
					release()
				}
				if reserved {
					_, cleanup, reserveErr := ReserveEmbedding(ctx, []string{"test"})
					err = reserveErr
					if cleanup != nil {
						cleanup()
					}
				} else {
					req, reqErr := http.NewRequestWithContext(
						ctx, http.MethodPost, "https://gateway.invalid/embeddings",
						strings.NewReader(`{"input":["test"]}`),
					)
					if reqErr != nil {
						t.Fatal(reqErr)
					}
					transport := Transport{Base: forbiddenBudgetWire{t}, Embedding: true}
					_, err = transport.RoundTrip(req)
				}
				if !errors.Is(err, ErrBudgetBusy) {
					t.Fatalf("known exhausted allocation lost its retryable category: %v", err)
				}
				if g.Check(context.Background()) != nil ||
					client.Exists(context.Background(), g.key("trip")).Val() != 0 {
					t.Fatal("known local backpressure globally tripped unrelated sources")
				}
				if client.ZCard(context.Background(), g.key("window")).Val() != 1 {
					t.Fatal("denied request changed RPM/TPM accounting")
				}
			})
		}
	}
}

type forbiddenBudgetWire struct{ t *testing.T }

func (f forbiddenBudgetWire) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("request escaped a full shared budget")
	return nil, errors.New("unexpected wire call")
}
