package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
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
	kb, err := p.loadAuthorizationKB(ctx, row.KnowledgeBaseID)
	if err != nil || kb.TenantID != row.TenantID {
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
	_, resource, attempt, err := recovery.exactRetrySubject(ctx, id)
	if err != nil {
		return true, nil, exactRetryPublicError(err)
	}
	if resource.ActiveVersionID != "" || attempt.Status == "indexed" {
		return true, nil, apperrors.NewConflictError("该个人飞书文档已发布；普通重建不会清理已发布内容").
			WithDetails(map[string]any{"reason": "personal_exact_published_rebuild_requires_versioned_protocol"})
	}
	ticket, err := recovery.requestExactRetry(ctx, id, true)
	if err != nil {
		return true, nil, exactRetryPublicError(err)
	}
	return true, &types.PersonalExactRetryAccepted{
		KnowledgeID: id, OperationID: ticket.ID, State: ticket.Op, Recovery: "personal_exact_retry",
	}, nil
}
