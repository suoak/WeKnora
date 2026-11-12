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
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		owner, types.OrgRoleViewer, s.personalRecovery.shares, nil)
	if err != nil {
		return nil, err
	}
	result := &types.KnowledgeBase{}
	fields := `
id,tenant_id,creator_id,name,type,embedding_model_id,summary_model_id,vector_store_id,
storage_backend_id,
CASE WHEN storage_provider_config IS NULL THEN NULL ELSE
 jsonb_build_object('provider',storage_provider_config->>'provider') END AS storage_provider_config,
indexing_strategy,chunking_config,faq_config,image_processing_config,extract_config,
question_generation_config,auto_tag_config,profile_config,asr_config,wiki_config,
CASE WHEN vlm_config IS NULL THEN NULL ELSE vlm_config::jsonb - 'api_key' END AS vlm_config,
created_at,updated_at`
	if grant.Permission.HasPermission(types.OrgRoleEditor) &&
		grant.Caller.Role.HasPermission(types.TenantRoleContributor) {
		// Settings must round-trip the existing configuration rather than
		// replacing omitted fields with editor defaults. Generated document
		// profiles and storage credentials remain outside metadata responses.
		fields += ",description"
	}
	err = s.personalRecovery.service.db.WithContext(ctx).Table("knowledge_bases").Select(fields).
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
