package feishusynccontrol

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticServiceRejectsLegacyPurposeBeforeDependencyRead(t *testing.T) {
	// No controller/Redis is installed: rejection belongs to protocol selection,
	// not availability of a legacy ticket or its old infrastructure.
	m := &RecoveryManager{system: &automaticService{}}
	for _, worker := range []bool{false, true} {
		ctx, raw, err := m.reconciliationContext(
			context.Background(),
			uuid.NewString(),
			uuid.NewString(),
			worker,
		)
		require.ErrorIs(t, err, ErrRecoveryRejected)
		require.ErrorContains(t, err, "legacy_reconciliation_not_applicable")
		require.Empty(t, raw)
		require.Empty(t, ReconciliationPolicy(ctx, "source"))
	}
}

func TestAutomaticServiceDoesNotReinterpretRetainedSignedReconciliation(t *testing.T) {
	f, plan, source := reconciliationFixture(t)
	ctx := reconciliationOperator()
	work, err := f.m.BeginSourceReconciliation(ctx, source, plan.ID)
	require.NoError(t, err)
	run := uuid.NewString()
	require.NoError(t, f.m.BindSourceReconciliationRun(work, source, run))
	claimed := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: source, SyncLogID: run, TenantID: f.authority[source].TenantID, Generation: 1,
	})
	legacy, err := RestoreSourceReconciliation(claimed, source, plan.ID)
	require.NoError(t, err)
	require.True(t, types.LegacyReconciliationRead(legacy, source))
	before := f.r.Get(ctx, f.m.controller.prefix+"recovery").Val()
	ticket := f.r.HGetAll(ctx, f.m.reconciliationKey(plan.ID, source)).Val()
	queue, err := f.m.queue(ctx)
	require.NoError(t, err)
	// The same valid old ticket cannot acquire a special read purpose once
	// ordinary service is system-managed. Expiration/signature checks must not
	// be silently replaced by the new ordinary-source validator.
	f.m.system = &automaticService{}
	_, err = f.m.BeginSourceReconciliation(ctx, source, plan.ID)
	require.ErrorIs(t, err, ErrRecoveryRejected)
	err = f.m.BindSourceReconciliationRun(work, source, run)
	require.ErrorIs(t, err, ErrRecoveryRejected)
	_, _, err = f.m.ReconciliationAuthorization(work, source)
	require.ErrorIs(t, err, ErrRecoveryRejected)
	restored, err := RestoreSourceReconciliation(claimed, source, plan.ID)
	require.ErrorIs(t, err, ErrRecoveryRejected)
	require.False(t, types.LegacyReconciliationRead(restored, source))
	require.Empty(t, ReconciliationPolicy(restored, source))
	require.Equal(t, before, f.r.Get(ctx, f.m.controller.prefix+"recovery").Val())
	require.Equal(t, ticket, f.r.HGetAll(ctx, f.m.reconciliationKey(plan.ID, source)).Val())
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, queue.digest, after.digest)
	require.Equal(t, queue.fields, after.fields)
}
