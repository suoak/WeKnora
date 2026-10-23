package feishusafety

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// SplitEmbeddingInputs tightens vendor batches for marked Feishu work. A chunk
// is never truncated: it must individually fit the shared conservative TPM cap.
func SplitEmbeddingInputs(ctx context.Context, texts []string) ([][]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if !Scoped(ctx) {
		return [][]string{texts}, nil
	}
	g := Default()
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	var out [][]string
	start, total := 0, 0
	for i, input := range texts {
		n := len(input) + g.config.TokenOverhead
		if n > g.config.TPM {
			return nil, fmt.Errorf("%w: one embedding chunk exceeds token allocation", ErrClosed)
		}
		if i > start && (i-start == 20 || total+n > g.config.TPM) {
			out = append(out, texts[start:i])
			start = i
			total = 0
		}
		total += n
	}
	return append(out, texts[start:]), nil
}

type (
	reservationKey       struct{}
	embeddingReservation struct {
		mu      sync.Mutex
		guard   *Guard
		digest  [32]byte
		tokens  int
		phase   int // 0 reserved, 1 owned by wire, 2 released
		release func()
	}
)

func inputDigest(inputs []string) [32]byte { raw, _ := json.Marshal(inputs); return sha256.Sum256(raw) }

// EmbeddingResultFailed belongs at the embedding response boundary, never the
// datasource boundary. Error bodies are not inspected or logged. Deliberate
// caller cancellation and graceful local drain are not new global incidents.
func EmbeddingResultFailed(ctx context.Context, reason string) {
	if !Scoped(ctx) || ctx.Err() != nil {
		return
	}
	g := Default()
	if g == nil || g.localClosed.Load() {
		return
	}
	trip(g, reason)
}

// ReserveEmbedding waits BEFORE the SDK/http.Client request timeout begins.
// Cleanup releases only an unused ticket; a consumed ticket belongs to the wire
// until its body is closed. RPM/TPM are intentionally never refunded.
func ReserveEmbedding(ctx context.Context, inputs []string) (context.Context, func(), error) {
	if !Scoped(ctx) {
		return ctx, func() {}, nil
	}
	g := Default()
	if err := g.Check(ctx); err != nil {
		return ctx, nil, err
	}
	if len(inputs) == 0 || len(inputs) > 20 {
		return ctx, nil, ErrClosed
	}
	tokens := 0
	for _, input := range inputs {
		tokens += len(input) + g.config.TokenOverhead
	}
	release, err := g.begin(ctx, tokens)
	if err != nil {
		if ctx.Err() == nil {
			trip(g, "embedding_reservation_unavailable")
		}
		return ctx, nil, err
	}
	ticket := &embeddingReservation{guard: g, digest: inputDigest(inputs), tokens: tokens, release: release}
	cleanup := func() {
		ticket.mu.Lock()
		defer ticket.mu.Unlock()
		if ticket.phase == 0 {
			ticket.phase = 2
			ticket.release()
		}
	}
	return context.WithValue(ctx, reservationKey{}, ticket), cleanup, nil
}

func hasReservation(ctx context.Context) bool {
	_, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	return ok
}

func consumeReservation(ctx context.Context, g *Guard, inputs []string) (func(), bool, error) {
	ticket, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	if !ok {
		return nil, false, nil
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()
	tokens := 0
	for _, input := range inputs {
		tokens += len(input) + g.config.TokenOverhead
	}
	if ticket.phase != 0 || ticket.guard != g || ticket.tokens != tokens || ticket.digest != inputDigest(inputs) {
		return nil, true, fmt.Errorf("%w: reused or mismatched embedding reservation", ErrClosed)
	}
	ticket.phase = 1
	return func() {
		ticket.mu.Lock()
		defer ticket.mu.Unlock()
		if ticket.phase == 1 {
			ticket.phase = 2
			ticket.release()
		}
	}, true, nil
}

// WaitEmbeddingRetry is invoked outside the HTTP timeout by protocol callers.
// It shares the bounded three-attempt/30s delay policy with direct transports.
func WaitEmbeddingRetry(ctx context.Context, retryAfter string, attempt int, waited *time.Duration) error {
	g := Default()
	if err := g.Check(ctx); err != nil {
		return err
	}
	if attempt >= 2 {
		trip(g, "embedding_429_retries_exhausted")
		return ErrClosed
	}
	delay := (time.Second << attempt) + time.Duration(rand.Int64N(int64(100*time.Millisecond)))
	if retryAfter != "" {
		var after time.Duration
		if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds >= 0 {
			if seconds > 30 {
				after = 31 * time.Second
			} else {
				after = time.Duration(seconds) * time.Second
			}
		} else if date, err := http.ParseTime(retryAfter); err == nil {
			after = time.Until(date)
		} else {
			trip(g, "embedding_retry_after_unknown")
			return ErrClosed
		}
		if after > delay {
			delay = after
		}
	}
	if *waited+delay > 30*time.Second {
		trip(g, "embedding_retry_wait_exceeded")
		return ErrClosed
	}
	*waited += delay
	if err := waitPermit(ctx, g, delay); err != nil {
		if ctx.Err() == nil {
			trip(g, "embedding_retry_state_unavailable")
		}
		return err
	}
	return nil
}
