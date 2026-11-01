package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
)

// ValidateKnowledgeReparse checks protocol metadata before any entity/body
// lookup or cleanup. A channel or a client-supplied flag is never authority.
func (s *knowledgeService) ValidateKnowledgeReparse(ctx context.Context, id string) error {
	row, _, err := s.authorizePersonalReparse(ctx, id)
	if err != nil || row == nil {
		return err
	}
	return apperrors.NewConflictError("个人飞书导入文档不能使用普通重建；" +
		"已发布内容保留，请使用受控来源恢复操作。").
		WithDetails(map[string]any{"reason": "personal_exact_recovery_required", "destructive_reparse_allowed": false})
}

func (s *knowledgeService) authorizePersonalReparse(
	ctx context.Context, id string,
) (*repository.PersonalReparseMetadata, *PersonalRecovery, error) {
	reader, ok := s.repo.(interface {
		LoadPersonalReparseMetadata(context.Context, string) (*repository.PersonalReparseMetadata, error)
	})
	if !ok {
		return nil, nil, apperrors.NewServiceUnavailableError("Document protocol verification is unavailable")
	}
	row, err := reader.LoadPersonalReparseMetadata(ctx, id)
	if err != nil {
		return nil, nil, apperrors.NewServiceUnavailableError("Document protocol verification is unavailable")
	}
	if row.ID == "" {
		return nil, nil, nil
	}
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok {
		return nil, nil, apperrors.NewServiceUnavailableError("Document recovery authorization is unavailable")
	}
	caller := types.CallerFromContext(ctx)
	principal, valid := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !valid || principal.Type != types.PrincipalWebUser ||
		principal.ID == "" || principal.ID != caller.UserID {
		return nil, nil, apperrors.NewForbiddenError("Document operation denied")
	}
	// This handler precedes the ordinary ownership middleware, whose full
	// Knowledge lookup is intentionally blocked for these documents. Preserve
	// that same creator-or-Admin rule using live identity metadata, never a
	// claimed role or the source owner's independent OAuth authority.
	var member bool
	if err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(
SELECT 1 FROM tenant_members m JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id
WHERE m.user_id=? AND m.tenant_id=? AND m.role=? AND m.status='active' AND m.deleted_at IS NULL
AND u.is_active AND u.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL)`,
		caller.UserID, caller.TenantID, caller.Role).Scan(&member).Error; err != nil || !member {
		return nil, nil, apperrors.NewForbiddenError("Document operation denied")
	}
	kb, err := p.loadAuthorizationKB(ctx, row.KnowledgeBaseID)
	if err != nil || kb.TenantID != row.TenantID {
		return nil, nil, apperrors.NewForbiddenError("Document operation denied")
	}
	if !caller.Role.HasPermission(types.TenantRoleAdmin) &&
		(!caller.Role.HasPermission(types.TenantRoleContributor) || kb.CreatorID != caller.UserID) {
		return nil, nil, apperrors.NewForbiddenError("Document operation denied")
	}
	if _, err = access.ResolveKB(
		ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor, p.shares, nil,
	); err != nil {
		return nil, nil, apperrors.NewForbiddenError("Document operation denied")
	}
	return row, p, nil
}

// TryPersonalReparse handles the source-specific action before the ordinary
// destructive route. A normal authenticated KB editor is mandatory; the file
// owner's live OAuth grant remains an independent source authority.
func (s *knowledgeService) TryPersonalReparse(
	ctx context.Context, id string,
) (bool, *types.PersonalExactRetryAccepted, error) {
	row, recovery, err := s.authorizePersonalReparse(ctx, id)
	if err != nil {
		return true, nil, err
	}
	if row == nil {
		return false, nil, nil
	}
	imported, resource, attempt, err := recovery.exactRetrySubject(ctx, id)
	if err != nil {
		return true, nil, exactRetryPublicError(err)
	}
	if resource.ActiveVersionID != "" || attempt.Status == "indexed" {
		if _, err := recovery.prepareExactPersonalSource(ctx, attempt, true); err != nil {
			return true, nil, personalSourceOperationError(ctx, imported.UserID, err)
		}
		if err := feishusafety.CheckLegacy(ctx, "feishu", imported.ID); err != nil {
			return true, nil, apperrors.NewServiceUnavailableError("飞书同步服务暂不可用，已发布内容仍可正常查看").
				WithDetails(map[string]any{"reason": "personal_source_sync_temporarily_unavailable"})
		}
		return true, nil, apperrors.NewConflictError("此文档暂不能安全同步更新，已发布内容完整保留").
			WithDetails(map[string]any{"reason": "personal_exact_published_rebuild_requires_versioned_protocol"})
	}
	ticket, err := recovery.requestExactRetry(ctx, id, true)
	if err != nil {
		return true, nil, personalSourceOperationError(ctx, imported.UserID, err)
	}
	return true, &types.PersonalExactRetryAccepted{
		KnowledgeID: id, OperationID: ticket.ID, State: ticket.Op, Recovery: "personal_exact_retry",
	}, nil
}

func personalSourceOperationError(ctx context.Context, sourceOwner string, err error) error {
	if errors.Is(err, ErrFeishuReconnect) || errors.Is(err, core.ErrAuth) {
		caller := types.CallerFromContext(ctx)
		allowed := caller.UserID != "" && caller.UserID == sourceOwner
		message := "飞书授权已失效，请联系原导入用户重新授权；原文件保持不变"
		if allowed {
			message = "飞书授权已失效，请重新授权后重试；原文件保持不变"
		}
		return apperrors.NewForbiddenError(message).WithDetails(map[string]any{
			"reason": "personal_exact_source_owner_reauthorization_required", "reauthorize_allowed": allowed,
		})
	}
	return exactRetryPublicError(err)
}
