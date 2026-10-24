package feishusafety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Transport surrounds the existing SSRF-safe transport, preserving its DNS,
// redirect and address protections. Each underlying retry is admitted separately.
type Transport struct {
	Base        http.RoundTripper
	Embedding   bool
	retrySleep  func(context.Context, time.Duration) error
	retryJitter func() time.Duration
}

func trip(g *Guard, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = g.Trip(ctx, reason)
}

// A caller may cancel one source after a claim/version failure or an explicit
// task cancellation. That must drain this request without disabling unrelated
// sources. Deadlines and errors not caused by that caller remain fail-closed.
// Reasons are fixed categories; never include URLs, response bodies or errors.
func tripRequestFailure(caller context.Context, g *Guard, err error, embedding bool, phase string) {
	if g.localClosed.Load() ||
		(errors.Is(err, context.Canceled) && errors.Is(caller.Err(), context.Canceled)) {
		return
	}
	family, category := "feishu", "io_failure"
	if embedding {
		family = "embedding"
	}
	var networkError net.Error
	timedOut := errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &networkError) && networkError.Timeout())
	if !embedding && timedOut {
		// Source HTTP timeouts belong to the existing connector's bounded
		// retry/backoff. No response is accepted and no candidate is published.
		// A healthy shared gate is required even for this local classification.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if g.Check(ctx) != nil {
			trip(g, "feishu_"+phase+"_timeout_state_unconfirmed")
		}
		return
	}
	switch {
	case timedOut:
		category = "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF):
		category = "unexpected_eof"
	case errors.Is(err, context.Canceled):
		category = "canceled_without_caller"
	}
	trip(g, family+"_"+phase+"_"+category)
}

// RoundTrip owns the only retry budget for marked Embedding 429 responses.
// A terminal failure latches the gate, so protocol transport retries cannot
// generate additional network attempts. Each retry reacquires all wire budgets.
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Protocol-owned reservations also own retry/queue waits, outside Client.Do.
	if t.Embedding && Scoped(req.Context()) && hasReservation(req.Context()) {
		return t.roundTripAttempt(req)
	}
	if !t.Embedding || !Scoped(req.Context()) {
		return t.roundTripAttempt(req)
	}
	if req.Body == nil {
		return nil, ErrClosed
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, 64*1024*1024+1))
	_ = req.Body.Close()
	if err != nil || len(raw) > 64*1024*1024 {
		return nil, ErrClosed
	}
	var waited time.Duration
	for attempt := 0; attempt < 3; attempt++ {
		wire := req.Clone(req.Context())
		wire.Body = io.NopCloser(bytes.NewReader(raw))
		resp, err := t.roundTripAttempt(wire)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 429 {
			return resp, nil
		}
		g := Default()
		if attempt == 2 {
			trip(g, "embedding_429_retries_exhausted")
			return nil, ErrClosed
		}
		delay := time.Second << attempt
		jitter := time.Duration(rand.Int64N(int64(100 * time.Millisecond)))
		if t.retryJitter != nil {
			jitter = t.retryJitter()
		}
		delay += jitter
		if value := resp.Header.Get("Retry-After"); value != "" {
			var after time.Duration
			if seconds, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil && seconds >= 0 {
				if seconds > 30 {
					after = 31 * time.Second
				} else {
					after = time.Duration(seconds) * time.Second
				}
			} else if date, parseErr := http.ParseTime(value); parseErr == nil {
				after = time.Until(date)
			} else {
				trip(g, "embedding_retry_after_unknown")
				return nil, ErrClosed
			}
			if after > delay {
				delay = after
			}
		}
		if waited+delay > 30*time.Second {
			trip(g, "embedding_retry_wait_exceeded")
			return nil, ErrClosed
		}
		waited += delay
		if t.retrySleep != nil {
			err = t.retrySleep(req.Context(), delay)
		} else {
			err = waitPermit(req.Context(), g, delay)
		}
		if err != nil {
			if req.Context().Err() == nil {
				trip(g, "embedding_retry_state_unavailable")
			}
			return nil, err
		}
	}
	return nil, ErrClosed
}

func waitPermit(ctx context.Context, g *Guard, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := g.Check(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return g.Check(ctx)
		case <-ticker.C:
		}
	}
}

func (t Transport) roundTripAttempt(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if !Scoped(req.Context()) {
		return base.RoundTrip(req)
	}
	g := Default()
	if err := g.Check(req.Context()); err != nil {
		return nil, err
	}
	release := func() {}
	if t.Embedding {
		if !strings.EqualFold(req.URL.Host, g.config.EndpointHost) {
			trip(g, "embedding_endpoint_mismatch")
			return nil, ErrClosed
		}
		if req.Body == nil {
			return nil, fmt.Errorf("%w: missing embedding body", ErrClosed)
		}
		raw, err := io.ReadAll(io.LimitReader(req.Body, 64*1024*1024+1))
		_ = req.Body.Close()
		if err != nil || len(raw) > 64*1024*1024 {
			return nil, fmt.Errorf("%w: unreadable embedding body", ErrClosed)
		}
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(raw))
		var payload struct {
			Input json.RawMessage `json:"input"`
		}
		if err = json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("%w: invalid embedding body", ErrClosed)
		}
		var inputs []string
		if err = json.Unmarshal(payload.Input, &inputs); err != nil {
			var single string
			if err = json.Unmarshal(payload.Input, &single); err != nil {
				return nil, fmt.Errorf("%w: unsupported token input", ErrClosed)
			}
			inputs = []string{single}
		}
		if len(inputs) == 0 || len(inputs) > 20 {
			return nil, fmt.Errorf("%w: empty embedding input", ErrClosed)
		}
		tokens := 0
		for _, input := range inputs {
			tokens += len(input) + g.config.TokenOverhead
		}
		var reserved bool
		release, reserved, err = consumeReservation(req.Context(), g, inputs)
		if !reserved {
			release, err = g.begin(req.Context(), tokens)
		}
		if err != nil {
			if req.Context().Err() == nil && !errors.Is(err, ErrBudgetBusy) {
				trip(g, "embedding_budget_or_state_unavailable")
			}
			return nil, err
		}
	}
	caller := req.Context()
	ctx, cancel := context.WithCancel(caller)
	req = req.Clone(ctx)
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done); cancel(); release() }) }
	// Controller/Redis loss cancels requests already in transport or body reads.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(ctx, 200*time.Millisecond)
				checked := make(chan error, 1)
				go func() { checked <- g.Check(checkCtx) }()
				var err error
				select {
				case err = <-checked:
				case <-checkCtx.Done():
					err = checkCtx.Err()
				}
				checkCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	resp, err := base.RoundTrip(req)
	if err != nil {
		finish()
		tripRequestFailure(caller, g, err, t.Embedding, "headers")
		return nil, err
	}
	if t.Embedding && resp.StatusCode == 429 {
		_ = resp.Body.Close()
		finish()
		resp.Body = http.NoBody
		return resp, nil
	}
	// Invalid source queries/identities and missing/inaccessible resources are
	// connector-level failures. Preserve status/body for the connector's typed
	// diagnosis without globally stopping independent sources or identities.
	if !t.Embedding && (resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusNotFound) {
		if resp.Body == nil {
			finish()
		} else {
			resp.Body = &guardedBody{
				ReadCloser: resp.Body, finish: finish, guard: g, caller: caller, embedding: t.Embedding,
			}
		}
		return resp, nil
	}
	// Fail closed for all non-2xx, including quota errors not expressed as 429.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		finish()
		family := "feishu"
		if t.Embedding {
			family = "embedding"
		}
		trip(g, fmt.Sprintf("%s_http_%d", family, resp.StatusCode))
		return nil, ErrClosed
	}
	if resp.Body == nil {
		finish()
		return resp, nil
	}
	resp.Body = &guardedBody{
		ReadCloser: resp.Body, finish: finish, guard: g, caller: caller, embedding: t.Embedding,
	}
	return resp, nil
}

type guardedBody struct {
	io.ReadCloser
	finish    func()
	guard     *Guard
	caller    context.Context
	embedding bool
}

func (b *guardedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finish()
		if err != io.EOF {
			tripRequestFailure(b.caller, b.guard, err, b.embedding, "response")
		}
	}
	return n, err
}
func (b *guardedBody) Close() error { err := b.ReadCloser.Close(); b.finish(); return err }
