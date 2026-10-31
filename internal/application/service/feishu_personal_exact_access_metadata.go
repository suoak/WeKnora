package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// GetKnowledgeBaseAccessMetadata supplies ownership and configuration to canonical access
// resolution for recovered Web reads. It grants no KB body/profile authority.
func (s *knowledgeBaseService) GetKnowledgeBaseAccessMetadata(
	ctx context.Context, kb string,
) (*types.KnowledgeBase, error) {
	principal, web := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !exactPersonalRecoveryEnabled() || s.personalRecovery == nil || !web ||
		principal.Type != types.PrincipalWebUser || principal.ID == "" ||
		types.CallerFromContext(ctx).UserID != principal.ID {
		return s.GetKnowledgeBaseByID(ctx, kb)
	}
	var count int64
	if err := s.personalRecovery.service.db.WithContext(ctx).Table("task_pending_ops").Where(
		"scope=? AND payload->>'knowledge_base_id'=?", types.PersonalExactReceiptScope, kb).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return s.GetKnowledgeBaseByID(ctx, kb)
	}
	owner, err := s.personalRecovery.loadAuthorizationKB(ctx, kb)
	if err != nil {
		return nil, err
	}
	if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		owner, types.OrgRoleViewer, s.personalRecovery.shares, nil); err != nil {
		return nil, err
	}
	result := &types.KnowledgeBase{}
	err = s.personalRecovery.service.db.WithContext(ctx).Table("knowledge_bases").Select(`
id,tenant_id,creator_id,name,type,embedding_model_id,summary_model_id,vector_store_id,
indexing_strategy,chunking_config,faq_config,created_at,updated_at`).
		Where("id=? AND tenant_id=? AND deleted_at IS NULL", kb, owner.TenantID).Scan(result).Error
	if err != nil {
		return nil, err
	}
	if result.ID != kb {
		return nil, errPersonalExactDocument
	}
	result.PrivateOwnerID = owner.PrivateOwnerID
	return result, nil
}
