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
		message        string
	}{
		{1, "A", "attempt-a", 1069902, "no permission"},
		{2, "A", "attempt-a", 99991679, "Forbidden"},
		{1, "B", "attempt-a", 99991672, "Forbidden"},
		{1, "A", "other-attempt", 99991668, "Forbidden"},
	} {
		require.NoError(t, s.db.Create(&types.AuditLog{
			TenantID: tc.tenant, ActorUserID: tc.owner, TargetID: m.ID, ScopeID: m.KBID,
			Action: "feishu.api_request_failed", Details: types.JSON(fmt.Sprintf(
				`{"attempt_id":%q,"code":%d,"provider_message":%q,`+
					`"operation":"create_export_task","request_id_hash":"123456789abc"}`,
				tc.attempt, tc.code, tc.message,
			)),
		}).Error)
	}
	for _, tc := range []struct{ category, action string }{
		{"source_copy_export_denied", "contact_document_owner"},
		{"source_copy_only_requires_policy", "check_export_policy"},
	} {
		t.Run(tc.category, func(t *testing.T) {
			item := &types.FeishuBatchItem{ErrorCategory: tc.category}
			s.decorateFeishuFailure(context.Background(), item, m, "attempt-a", 1)
			require.Equal(t, 1069902, item.ProviderCode)
			require.Equal(t, "no permission", item.ProviderMessage)
			require.Equal(t, "create_export_task", item.Operation)
			require.Equal(t, "123456789abc", item.RequestIDHash)
			require.Equal(t, tc.action, item.UserAction)
			require.False(t, item.Retryable)
		})
	}
	unknown := &types.FeishuBatchItem{ErrorCategory: "temporary_failure"}
	s.decorateFeishuFailure(context.Background(), unknown, m, "missing-attempt", 1)
	require.Zero(t, unknown.ProviderCode)
	require.Empty(t, unknown.Operation)
	require.True(t, unknown.Retryable)
	require.Equal(t, "wait_for_retry", unknown.UserAction)
}

func TestFeishuFailureDetailsOmitMissingOrUnsafeProviderText(t *testing.T) {
	s, _ := feishuFixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.AuditLog{}))
	m := &types.FeishuPersonalImport{ID: "import-a", UserID: "A", KBID: "kb-a"}
	for _, message := range []string{"", "no permission; access_token=private-value"} {
		require.NoError(t, s.db.Create(&types.AuditLog{
			TenantID: 1, ActorUserID: "A", TargetID: m.ID, ScopeID: m.KBID,
			Action: "feishu.api_request_failed", Details: types.JSON(fmt.Sprintf(
				`{"attempt_id":"attempt-a","code":1069902,"provider_message":%q}`, message,
			)),
		}).Error)
		item := &types.FeishuBatchItem{ErrorCategory: "source_copy_only_requires_policy"}
		s.decorateFeishuFailure(context.Background(), item, m, "attempt-a", 1)
		require.Equal(t, 1069902, item.ProviderCode)
		require.Empty(t, item.ProviderMessage)
	}
}
