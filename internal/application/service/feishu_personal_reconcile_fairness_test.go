package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersonalRecoveryReconcileDeadlineContinuesAfterInterruptedSource(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	first := f.prepare("https://team.feishu.cn/file/real")
	var original types.FeishuPersonalAttempt
	require.NoError(t, f.db.Where("id=?", first.AttemptID).Take(&original).Error)
	// Use an unrelated retained closed attempt as a later row. It is deliberately
	// not eligible for any recovery grant, and must remain byte-for-byte unchanged.
	later := original
	later.ID = "zz-last-unrelated-attempt"
	later.Status = "needs_attention"
	require.NoError(t, f.db.Create(&later).Error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var fired atomic.Bool
	name := "cancel_first_personal_reconcile_load"
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "feishu_personal_imports" && fired.CompareAndSwap(false, true) {
			cancel()
		}
	}))
	require.ErrorIs(t, f.p.Reconcile(ctx), context.Canceled)
	require.True(t, fired.Load())
	require.NoError(t, f.db.Callback().Query().Remove(name))
	require.Equal(t, original.ID, f.p.reconcileAfter)
	require.NoError(t, f.p.Reconcile(context.Background()))
	require.Empty(t, f.p.reconcileAfter, "reaching the tail wraps the next sweep")
	var saved types.FeishuPersonalAttempt
	require.NoError(t, f.db.Where("id=?", later.ID).Take(&saved).Error)
	require.Equal(t, later, saved, "cursor movement cannot authorize or mutate an unrelated attempt")
	// A subsequent complete sweep returns to the interrupted row.
	require.NoError(t, f.p.Reconcile(context.Background()))
	require.Empty(t, f.p.reconcileAfter)
}

func TestPersonalRecoveryReconcileCanceledAndConcurrentScansKeepCursor(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	f.p.reconcileAfter = "retained-position"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, f.p.Reconcile(ctx), context.Canceled)
	require.Equal(t, "retained-position", f.p.reconcileAfter)
	f.p.reconcileMu.Lock()
	done := make(chan error, 1)
	go func() { done <- f.p.Reconcile(context.Background()) }()
	require.NoError(t, <-done)
	require.Equal(t, "retained-position", f.p.reconcileAfter)
	f.p.reconcileMu.Unlock()
}
