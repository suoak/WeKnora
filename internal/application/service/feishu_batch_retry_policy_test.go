package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuBatchRetryPolicyDoesNotOfferExecutionReset(t *testing.T) {
	for _, tc := range []struct {
		name string
		item types.FeishuBatchItem
		want bool
	}{
		{"pre-admission", types.FeishuBatchItem{Status: "failed"}, true},
		{"execution", types.FeishuBatchItem{
			Status: "failed", ImportID: "existing",
			ErrorCategory: "exact_document_execution_requires_review",
		}, false},
		{"preflight", types.FeishuBatchItem{
			Status: "blocked", ImportID: "existing",
			ErrorCategory: "embedded_dependency_unsupported",
		}, false},
		{"identity", types.FeishuBatchItem{Status: "blocked", ErrorCategory: "source_identity_conflict"}, false},
		{"exhausted", types.FeishuBatchItem{Status: "failed", RetryCount: 3}, false},
		{"running", types.FeishuBatchItem{Status: "dispatched"}, false},
		{"publication-recheck", types.FeishuBatchItem{
			Status: "blocked", ImportID: "existing",
			ErrorCategory: "publication_confirmation_expired",
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, canRetryFeishuBatchItem(tc.item)) })
	}
}

func TestFeishuV2PostgresProgressShowsPreflightRefusal(t *testing.T) {
	f := v2PostgresFixture(t)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	m, _, attempt, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", attempt.ID).
		Updates(map[string]any{
			"status": "index_pending", "last_error_category": "embedded_dependency_unsupported",
		}).Error)
	item := types.FeishuBatchItem{Status: "dispatched", ImportID: m.ID}
	require.NoError(t, f.s.batchItemProgress(f.ctx, &item, m, time.Now(), &batchQueueProgress{}))
	require.Equal(t, "blocked", item.Status)
	require.Equal(t, "needs_attention", item.Stage)
	require.Equal(t, "embedded_dependency_unsupported", item.ErrorCategory)
	require.False(t, canRetryFeishuBatchItem(item))
}

func TestFeishuV2PostgresRetryRejectsNoop(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(f.ctx, "kb", "retry-noop-policy", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	item := view.Items[0]
	require.NoError(t, f.db.Where("id=?", item.ID).Take(&item).Error)
	require.NotEmpty(t, item.ImportID)
	require.NoError(t, f.db.Model(&item).Updates(map[string]any{
		"status": "failed", "error_category": "exact_document_execution_requires_review",
	}).Error)
	require.ErrorIs(t, f.s.RetryBatch(f.ctx, view.Batch.ID), ErrFeishuBatchNotRetryable)
	var after types.FeishuBatchItem
	require.NoError(t, f.db.Where("id=?", item.ID).Take(&after).Error)
	require.Equal(t, item.RetryCount, after.RetryCount)
	require.Equal(t, "failed", after.Status)
}
