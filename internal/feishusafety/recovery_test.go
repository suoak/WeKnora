package feishusafety

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecoveryBudgetAdmissionBindsExactRunAndPersistentMode(t *testing.T) {
	g, r := fixture(t, Config{})
	ctx := context.Background()
	g.RequireRecovery()
	state := map[string]any{
		"version":       1,
		"plan_id":       "plan",
		"plan_sha256":   "hash",
		"source_id":     "source",
		"config_sha256": "config",
		"phase":         "running",
		"run_id":        "run",
		"generation":    1,
		"expires_ms":    time.Now().Add(time.Minute).UnixMilli(),
	}
	raw, _ := json.Marshal(state)
	require.NoError(t, r.Set(ctx, g.key("recovery"), raw, 0).Err())
	require.NoError(t, r.Set(ctx, g.key("recovery_required"), "hash", 0).Err())
	work := WithRecoveryContext(
		WithScope(ctx, "source"),
		RecoveryContext{Plan: "plan", Config: "config", Run: "run", Generation: 1},
	)
	release, err := g.tryBegin(work, 1)
	require.NoError(t, err)
	release()
	stale := WithRecoveryExecution(work, "config", "run", 2)
	_, err = g.tryBegin(stale, 1)
	require.ErrorIs(t, err, ErrClosed)
	require.NoError(t, r.Del(ctx, g.key("recovery")).Err())
	_, err = g.tryBegin(work, 1)
	require.ErrorIs(t, err, ErrClosed)
	require.NoError(t, r.Del(ctx, g.key("recovery_required")).Err())
	_, err = g.tryBegin(work, 1)
	require.ErrorIs(t, err, ErrClosed, "local replacement remains closed even if both keys are lost")
}
