package feishusafety

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
		failure error
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
	if !g.recordProviderFailure(ctx, reason, "") {
		trip(g, reason)
	}
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
		return ctx, nil, embeddingAdmissionFailure(ctx, g, err, "embedding_reservation_unavailable")
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

// Both reservation and direct transports reach this only after full source
// admission passed. A later source probe failure must deny this request, but
// can remain local if the exact distributed runtime fence is still healthy.
func embeddingAdmissionFailure(ctx context.Context, g *Guard, err error, reason string) error {
	if errors.Is(err, ErrControlTemporarilyUnavailable) {
		return err
	}
	if errors.Is(err, ErrClosed) && g.DependencySuspended() {
		return ErrControlTemporarilyUnavailable
	}
	diagnostic := err
	if errors.Is(err, errSourceProbeFailed) && ctx.Err() == nil {
		check, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		controlErr := g.checkRuntime(check, true)
		cancel()
		if controlErr == nil {
			return fmt.Errorf("%w: %w", ErrSourceValidationUnavailable, err)
		}
		diagnostic = controlErr
	}
	if ctx.Err() == nil && !errors.Is(err, ErrBudgetBusy) && !errors.Is(err, ErrSourceAdmissionChanged) {
		stop, finish := context.WithTimeout(context.Background(), time.Second)
		defer finish()
		_ = g.tripUsingDetails(stop, g.client, reason, "", embeddingAdmissionCode(diagnostic))
	}
	return err
}

func hasReservation(ctx context.Context) bool {
	_, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	return ok
}

// EmbeddingRequestError preserves a transport-owned local refusal across
// http.Client's timeout wrapper, which can replace the original error chain.
// A reservation can carry this classification only from the guarded transport.
func EmbeddingRequestError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	ticket, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	if !ok {
		return err
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()
	if ticket.guard == Default() && ticket.failure != nil {
		failure := ticket.failure
		ticket.failure = nil
		return fmt.Errorf("%w: %w", failure, err)
	}
	return err
}

func recordEmbeddingHeaderTimeout(ctx context.Context) {
	ticket, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	if !ok {
		return
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()
	if ticket.guard == Default() && ticket.phase == 2 {
		ticket.failure = ErrEmbeddingTemporarilyUnavailable
	}
}

func consumeReservation(ctx context.Context, g *Guard, inputs []string) (func(), bool, error) {
	ticket, ok := ctx.Value(reservationKey{}).(*embeddingReservation)
	if !ok {
		return nil, false, nil
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()
	ticket.failure = nil
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
		return providerRetryFailure(ctx, g, "embedding_429_retries_exhausted")
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
			return providerRetryFailure(ctx, g, "embedding_retry_after_unknown")
		}
		if after > delay {
			delay = after
		}
	}
	if *waited+delay > 30*time.Second {
		return providerRetryFailure(ctx, g, "embedding_retry_wait_exceeded")
	}
	*waited += delay
	if err := waitPermit(ctx, g, delay); err != nil {
		if ctx.Err() == nil && !errors.Is(err, ErrSourceAdmissionChanged) {
			_ = providerRetryFailure(ctx, g, "embedding_retry_state_unavailable")
		}
		return err
	}
	return nil
}
