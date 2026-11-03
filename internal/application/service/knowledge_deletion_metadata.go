package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

type knowledgeDeleteActorKey struct{}

// GetKnowledgeDeletionMetadata supplies the persisted identity to DELETE's
// existing guards, without loading source content or requiring publication.
func (s *knowledgeService) GetKnowledgeDeletionMetadata(ctx context.Context, id string) (*types.Knowledge, error) {
	if !types.IsKnowledgeDeletionMetadata(ctx, id) {
		return nil, apperrors.NewForbiddenError("Deletion metadata scope required")
	}
	tenant, err := writeExecutionTenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.GetKnowledgeDeletionMetadata(ctx, tenant, []string{id})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0].ID != id || rows[0].TenantID != tenant {
		return nil, apperrors.NewNotFoundError("Knowledge not found")
	}
	return rows[0], nil
}

func (s *knowledgeService) knowledgeOwnerMetadata(
	ctx context.Context, tenant uint64, id string,
) (*types.Knowledge, error) {
	if !types.IsKnowledgeDeletionMetadata(ctx, id) {
		return s.repo.GetKnowledgeByID(ctx, tenant, id)
	}
	row, err := s.GetKnowledgeDeletionMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.TenantID != tenant {
		return nil, repository.ErrKnowledgeNotFound
	}
	return row, nil
}

// TaskInitiator is audit data, not a reusable Web grant. Revalidate the live
// actor before restoring the identity that the private-KB guard requires.
func (s *knowledgeService) restoreKnowledgeDeletionActor(
	ctx context.Context, tenant uint64, initiator types.TaskInitiator,
) (context.Context, error) {
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok || initiator.UserID == "" || initiator.APIKeyID != 0 || types.IsSyntheticUserID(initiator.UserID) {
		return ctx, nil
	}
	var member struct{ Role types.TenantRole }
	err := p.service.db.WithContext(ctx).Table("tenant_members m").Select("m.role").
		Joins("JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id").
		Where(`m.user_id=? AND m.tenant_id=? AND m.status='active' AND m.deleted_at IS NULL
AND u.is_active AND u.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL`,
			initiator.UserID, tenant).Take(&member).Error
	if err != nil || !member.Role.IsValid() {
		return ctx, apperrors.NewForbiddenError("Deletion actor is no longer authorized")
	}
	ctx = types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: initiator.UserID, Role: member.Role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: initiator.UserID})
	return context.WithValue(ctx, knowledgeDeleteActorKey{}, true), nil
}
