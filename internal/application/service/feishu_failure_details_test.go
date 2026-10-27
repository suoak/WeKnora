package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuFailureDetailsUseOnlyExactOwnerAttemptEvidence(t *testing.T) {
	s, _ := feishuFixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.AuditLog{}))
	m := &types.FeishuPersonalImport{ID: "import-a", UserID: "A", KBID: "kb-a"}
	for _, tc := range []struct {
		tenant         uint64
		owner, attempt string
		code           int
	}{
		{1, "A", "attempt-a", 1069902},
		{2, "A", "attempt-a", 99991679},
		{1, "B", "attempt-a", 99991672},
		{1, "A", "other-attempt", 99991668},
	} {
		require.NoError(t, s.db.Create(&types.AuditLog{
			TenantID: tc.tenant, ActorUserID: tc.owner, TargetID: m.ID, ScopeID: m.KBID,
			Action: "feishu.api_request_failed", Details: types.JSON(fmt.Sprintf(
				`{"attempt_id":%q,"code":%d,"operation":"create_export_task","request_id_hash":"123456789abc"}`,
				tc.attempt, tc.code,
			)),
		}).Error)
	}
	item := &types.FeishuBatchItem{ErrorCategory: "source_copy_export_denied"}
	s.decorateFeishuFailure(context.Background(), item, m, "attempt-a", 1)
	require.Equal(t, 1069902, item.ProviderCode)
	require.Equal(t, "create_export_task", item.Operation)
	require.Equal(t, "123456789abc", item.RequestIDHash)
	require.Equal(t, "contact_document_owner", item.UserAction)
	require.False(t, item.Retryable)
	unknown := &types.FeishuBatchItem{ErrorCategory: "temporary_failure"}
	s.decorateFeishuFailure(context.Background(), unknown, m, "missing-attempt", 1)
	require.Zero(t, unknown.ProviderCode)
	require.Empty(t, unknown.Operation)
	require.True(t, unknown.Retryable)
	require.Equal(t, "wait_for_retry", unknown.UserAction)
}
