package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

// watchDownloadProgress bounds headers and each gap between received bytes.
// It cancels the existing SSRF/safety-wrapped request rather than introducing
// another transport or accepting a partial body. The operation's total budget
// is independent and cannot be extended by a server dripping bytes forever.
func watchDownloadProgress(parent context.Context, idle time.Duration) (context.Context, func(), func()) {
	ctx, cancel := context.WithCancelCause(parent)
	var mu sync.Mutex
	last := time.Now()
	closed := false
	var timer *time.Timer
	mu.Lock()
	timer = time.AfterFunc(idle, func() {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		if remaining := idle - time.Since(last); remaining > 0 {
			timer.Reset(remaining)
			return
		}
		closed = true
		cancel(context.DeadlineExceeded)
	})
	mu.Unlock()
	progress := func() {
		mu.Lock()
		defer mu.Unlock()
		if !closed {
			last = time.Now()
			timer.Reset(idle)
		}
	}
	stop := func() {
		mu.Lock()
		closed = true
		timer.Stop()
		mu.Unlock()
		cancel(context.Canceled)
	}
	return ctx, progress, stop
}

func downloadDeadlineError(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
