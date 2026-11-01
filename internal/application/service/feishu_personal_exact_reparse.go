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
	reader, ok := s.repo.(interface {
		LoadPersonalReparseMetadata(context.Context, string) (*repository.PersonalReparseMetadata, error)
	})
	if !ok {
		return apperrors.NewServiceUnavailableError("Document protocol verification is unavailable")
	}
	row, err := reader.LoadPersonalReparseMetadata(ctx, id)
	if err != nil {
		return apperrors.NewServiceUnavailableError("Document protocol verification is unavailable")
	}
	if row.ID == "" {
		return nil
	}
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok {
		return apperrors.NewServiceUnavailableError("Document recovery authorization is unavailable")
	}
	caller := types.CallerFromContext(ctx)
	principal, valid := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !valid || principal.Type != types.PrincipalWebUser ||
		principal.ID == "" || principal.ID != caller.UserID {
		return apperrors.NewForbiddenError("Document operation denied")
	}
	kb, err := p.loadAuthorizationKB(ctx, row.KnowledgeBaseID)
	if err != nil || kb.TenantID != row.TenantID {
		return apperrors.NewForbiddenError("Document operation denied")
	}
	if _, err = access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor, p.shares, nil); err != nil {
		return apperrors.NewForbiddenError("Document operation denied")
	}
	return apperrors.NewConflictError("个人飞书导入文档不能使用普通重建；已发布内容保留，请使用受控来源恢复操作。").
		WithDetails(map[string]any{"reason": "personal_exact_recovery_required", "destructive_reparse_allowed": false})
}
