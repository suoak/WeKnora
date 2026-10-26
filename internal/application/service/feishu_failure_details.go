package service

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
)

var (
	safeFeishuOperation   = regexp.MustCompile(`^[a-z_]{1,80}$`)
	safeFeishuRequestHash = regexp.MustCompile(`^[a-f0-9]{12}$`)
)

func feishuFailureAction(category string) (string, bool) {
	switch category {
	case "authorization_expired", "reconnect_required", "authorization_invalid", "user_scope_missing",
		"refresh_token_expired", "refresh_rejected":
		return "connect_feishu", false
	case "app_scope_missing", "oauth_app_configuration":
		return "contact_app_admin", false
	case "source_read_denied", "source_copy_denied", "source_copy_export_denied", "source_permission_required",
		"source_metadata_denied", "export_permission_required", "export_create_denied", "export_download_denied":
		return "contact_document_owner", false
	case "kb_permission_required":
		return "request_kb_access", false
	case "sharing_approval_required":
		return "request_publication_approval", false
	case "temporary_failure":
		return "wait_for_retry", true
	case "":
		return "", false
	default:
		return "contact_admin", false
	}
}

// History diagnostics are read-only and tied to this owner's exact attempt.
// Missing historical audit evidence is left absent; it is never guessed from
// a generic permission error, and optional diagnostics cannot hide the task.
func (s *FeishuPersonalService) decorateFeishuFailure(ctx context.Context, item *types.FeishuBatchItem,
	m *types.FeishuPersonalImport, attemptID string, tenant uint64,
) {
	item.UserAction, item.Retryable = feishuFailureAction(item.ErrorCategory)
	if item.ErrorCategory == "" || m == nil || attemptID == "" || tenant == 0 ||
		!s.db.Migrator().HasTable(&types.AuditLog{}) {
		return
	}
	var row types.AuditLog
	if err := s.db.WithContext(ctx).Where(
		"tenant_id = ? AND actor_user_id = ? AND target_id = ? AND scope_id = ? AND action = ?"+
			" AND details->>'attempt_id' = ?",
		tenant, m.UserID, m.ID, m.KBID, "feishu.api_request_failed", attemptID,
	).Order("id DESC").First(&row).Error; err != nil {
		return
	}
	var evidence core.APIObservation
	if json.Unmarshal(row.Details, &evidence) != nil {
		return
	}
	if evidence.Code > 0 && evidence.Code < 1000000000 {
		item.ProviderCode = evidence.Code
	}
	if safeFeishuOperation.MatchString(evidence.Operation) {
		item.Operation = evidence.Operation
	}
	if safeFeishuRequestHash.MatchString(evidence.RequestIDHash) {
		item.RequestIDHash = evidence.RequestIDHash
	}
}
