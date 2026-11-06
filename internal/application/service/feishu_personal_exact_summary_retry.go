package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/utils"
)

// Retry inside the SAME fenced execution. The consumer retains its actual
// parser/model output proofs; replaying an abandoned staging receipt cannot
// reconstruct those proofs. Each run reacquires the normal live source checks.
func retryExactPersonalSummary(
	ctx context.Context, run func() error, wait func(context.Context, time.Duration) error,
) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run()
		if err == nil || attempt >= 2 || ctx.Err() != nil ||
			errors.Is(err, ErrFeishuReconnect) || errors.Is(err, core.ErrAuth) ||
			errors.Is(err, core.ErrPermission) || errors.Is(err, core.ErrAppScope) ||
			errors.Is(err, core.ErrUserScope) ||
			errors.Is(err, utils.ErrSSRFRequestRejected) ||
			errors.Is(err, feishusafety.ErrClosed) ||
			(!errors.Is(err, core.ErrTransient) && !errors.Is(err, core.ErrRateLimited)) {
			return err
		}
		if err := wait(ctx, time.Second<<uint(attempt+1)); err != nil {
			return err
		}
	}
}

func waitExactPersonalSummary(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
