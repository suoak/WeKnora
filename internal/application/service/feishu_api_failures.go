package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// These describe the failing operation, without claiming a missing edit role or
// conflating application/user scopes with a document's export security policy.
func feishuRemoteFailureCategory(err error) string {
	switch {
	case errors.Is(err, core.ErrPersonalBlocksPolicyUnverified):
		return "source_policy_verification_required"
	case errors.Is(err, core.ErrPersonalBlocksIncomplete):
		return "blocks_content_requires_review"
	case errors.Is(err, ErrFeishuRefreshExpired):
		return "refresh_token_expired"
	case errors.Is(err, ErrFeishuRefreshRejected):
		return "refresh_rejected"
	case errors.Is(err, ErrFeishuOAuthConfiguration):
		return "oauth_app_configuration"
	case errors.Is(err, core.ErrAppScope):
		return "app_scope_missing"
	case errors.Is(err, core.ErrUserScope):
		return "user_scope_missing"
	case errors.Is(err, core.ErrAuth):
		return "authorization_invalid"
	case errors.Is(err, ErrFeishuReconnect):
		return "reconnect_required"
	case errors.Is(err, core.ErrPermission):
		var api *core.APIError
		if errors.As(err, &api) {
			switch api.Operation {
			case "create_export_task":
				return "export_create_denied"
			case "get_export_task_status":
				return "export_status_denied"
			case "download_export_file", "download_export_file_range":
				return "export_download_denied"
			case "resolve_personal_resource", "get_wiki_node":
				return "source_metadata_denied"
			}
		}
		return "source_permission_required"
	}
	return ""
}

func (s *FeishuPersonalService) recordFeishuAPIError(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
	event core.APIObservation,
) {
	if event.Category == "" || s.audit == nil {
		return
	}
	// The typed observation excludes provider messages, URLs, tokens and body.
	details, err := json.Marshal(struct {
		AttemptID string `json:"attempt_id"`
		core.APIObservation
	}{a.ID, event})
	if err != nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.audit.Log(cleanup, &types.AuditLog{
		TenantID: a.CallerTenantID, ActorUserID: m.UserID,
		Action: types.AuditAction("feishu.api_request_failed"), Outcome: types.AuditOutcomeFailed,
		ScopeType: "knowledge_base", ScopeID: m.KBID, TargetType: "feishu_import", TargetID: m.ID,
		Details: types.JSON(details),
	}); err != nil {
		logger.Warnf(cleanup, "Feishu API failure audit could not be persisted")
	}
}
