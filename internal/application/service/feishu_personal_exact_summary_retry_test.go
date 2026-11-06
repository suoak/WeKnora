package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestExactPersonalSummaryRetryClassifiesFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		calls int
	}{
		{"transient", core.ErrTransient, 3},
		{"rate_limit", core.ErrRateLimited, 3},
		{"missing_policy", core.ErrPersonalBlocksPolicyUnverified, 1},
		{"permission", core.ErrPermission, 1},
		{"reconnect", ErrFeishuReconnect, 1},
		{"mixed_reconnect", errors.Join(core.ErrTransient, ErrFeishuReconnect), 1},
		{"mixed_permission", errors.Join(core.ErrTransient, core.ErrPermission), 1},
		{"closed_gate", errors.Join(core.ErrTransient, feishusafety.ErrClosed), 1},
		{"target_rejected", &core.APIError{
			Category: core.ErrorCategoryTransient, Cause: utils.ErrSSRFRequestRejected,
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			err := retryExactPersonalSummary(context.Background(), func() error {
				calls++
				return errors.Join(core.ErrPersonalBlocksPolicyUnverified, tc.cause)
			}, func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			})
			require.ErrorIs(t, err, tc.cause)
			require.Equal(t, tc.calls, calls)
			if tc.calls == 3 {
				require.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second}, delays)
			} else {
				require.Empty(t, delays)
			}
		})
	}
}

func TestExactPersonalSummaryRetryRecoversAndHonorsCancellation(t *testing.T) {
	calls := 0
	err := retryExactPersonalSummary(context.Background(), func() error {
		calls++
		if calls == 1 {
			return core.ErrTransient
		}
		return nil
	}, func(context.Context, time.Duration) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 2, calls)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	err = retryExactPersonalSummary(ctx, func() error {
		calls++
		return core.ErrTransient
	}, func(current context.Context, delay time.Duration) error {
		cancel()
		return waitExactPersonalSummary(current, delay)
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
