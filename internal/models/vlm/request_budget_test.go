package vlm

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/limiter"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type budgetProbeVLM struct {
	call func(context.Context)
}

func (budgetProbeVLM) GetModelID() string   { return "budget-probe" }
func (budgetProbeVLM) GetModelName() string { return "budget-probe" }
func (v budgetProbeVLM) Predict(ctx context.Context, _ [][]byte, _ string) (string, error) {
	v.call(ctx)
	return "complete", nil
}

func TestVLMConfiguredBudgetAppliesToEveryBackend(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{"", 180 * time.Second}, {"30", 30 * time.Second}, {"300", 300 * time.Second}, {"invalid", 180 * time.Second}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("VLM_HTTP_TIMEOUT_SECONDS", tc.value)
			ctx := logger.WithPrivatePayload(feishusafety.WithScope(context.Background(), "source"))
			start := time.Now()
			model, err := wrapVLMConcurrency(budgetProbeVLM{call: func(call context.Context) {
				deadline, ok := call.Deadline()
				require.True(t, ok)
				require.WithinDuration(t, start.Add(tc.want), deadline, time.Second)
				require.True(t, logger.PrivatePayload(call))
				require.True(t, feishusafety.Scoped(call))
			}}, 0, nil)
			require.NoError(t, err)
			_, err = model.Predict(ctx, nil, "")
			require.NoError(t, err)
		})
	}
}

func TestVLMBudgetPreservesEarlierTaskDeadlineAndCancellation(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := parent.Deadline()
	calls := 0
	model, err := wrapVLMConcurrency(budgetProbeVLM{call: func(ctx context.Context) {
		calls++
		deadline, _ := ctx.Deadline()
		require.Equal(t, want, deadline)
	}}, 0, nil)
	require.NoError(t, err)
	_, err = model.Predict(parent, nil, "")
	require.NoError(t, err)
	cancel()
	_, err = model.Predict(parent, nil, "")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

type budgetProbeLimiter struct {
	t        *testing.T
	released *bool
}

func (l budgetProbeLimiter) Acquire(ctx context.Context, _ string, _ int) (func(), error) {
	_, hasDeadline := ctx.Deadline()
	require.False(l.t, hasDeadline, "model timeout must start after queue admission")
	return func() { *l.released = true }, nil
}

func TestVLMBudgetStartsAfterQueueAdmission(t *testing.T) {
	released := false
	limiter.SetGovernor(budgetProbeLimiter{t: t, released: &released}, 1)
	t.Cleanup(func() { limiter.SetGovernor(nil, 0) })
	model, err := wrapVLMConcurrency(budgetProbeVLM{call: func(ctx context.Context) {
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		require.False(t, released)
	}}, 1, nil)
	require.NoError(t, err)
	_, err = model.Predict(types.WithBackgroundTask(context.Background()), nil, "")
	require.NoError(t, err)
	require.True(t, released)
}
