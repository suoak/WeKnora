package feishusynccontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalParentReconciliationActuallyReservesTicket(t *testing.T) {
	f, held := normalContinuationFixture(t, true)
	a := f.authority[held]
	a.SourceStatus = "active"
	f.authority[held] = a
	ctx := reconciliationOperator()
	p, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", nil, []string{held}, 1, 2*time.Hour)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.NoError(t, f.c.Step(context.Background()))
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
	require.NoError(t, f.m.checkSteadyState(ctx, state, time.Now().UnixMilli()), "active signed state")
	work, err := f.m.BeginSourceReconciliation(ctx, held, p.ID)
	require.NoError(t, err, "activation must permit actual controlled dispatch")
	require.Equal(t, p.ID, ReconciliationPolicy(work, held))
}
