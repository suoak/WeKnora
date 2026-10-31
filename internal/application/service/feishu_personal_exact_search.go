package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// A requested ID is a selector, never authority. Only an independently verified
// published receipt can issue the document capability used by normal retrieval.
func (s *knowledgeBaseService) exactSearchContext(
	ctx context.Context, kbID string, params types.SearchParams,
) (context.Context, error) {
	if _, _, _, _, exact := types.ExactPublishedDocumentIdentity(ctx); exact {
		_, err := types.CheckExactPublishedDocumentRead(ctx)
		return ctx, err
	}
	if !exactPersonalRecoveryEnabled() || s.personalRecovery == nil || len(params.KnowledgeIDs) != 1 {
		return ctx, nil
	}
	p := s.personalRecovery
	var count int64
	if err := p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).Where(
		"scope=? AND scope_id=?", types.PersonalExactReceiptScope, params.KnowledgeIDs[0],
	).Count(&count).Error; err != nil {
		return ctx, err
	}
	if count == 0 {
		return ctx, nil
	}
	read, err := p.exactPublishedReadContext(ctx, params.KnowledgeIDs[0])
	if err != nil {
		return ctx, err
	}
	_, _, _, boundKB, _ := types.ExactPublishedDocumentIdentity(read)
	if kbID != boundKB || (len(params.KnowledgeBaseIDs) != 0 &&
		(len(params.KnowledgeBaseIDs) != 1 || params.KnowledgeBaseIDs[0] != boundKB)) {
		return ctx, errPersonalExactDocument
	}
	return read, nil
}

// The exact capability does not open a KB entity or its generated profile.
// Normal store/model selection needs configuration only; canonical KB access is
// still checked by the caller and again by each use of the published proof.
func (s *knowledgeBaseService) exactSearchKnowledgeBase(
	ctx context.Context,
	kbID string,
) (*types.KnowledgeBase, error) {
	_, tenant, _, boundKB, exact := types.ExactPublishedDocumentIdentity(ctx)
	if !exact {
		return s.repo.GetKnowledgeBaseByID(ctx, kbID)
	}
	if s.personalRecovery == nil || boundKB != kbID {
		return nil, errPersonalExactDocument
	}
	if _, err := types.CheckExactPublishedDocumentRead(ctx); err != nil {
		return nil, err
	}
	p := s.personalRecovery
	owner, err := p.loadAuthorizationKB(ctx, kbID)
	if err != nil || owner.TenantID != tenant {
		return nil, errPersonalExactDocument
	}
	kb := &types.KnowledgeBase{}
	err = p.service.db.WithContext(ctx).Table("knowledge_bases").Select(`
id,tenant_id,creator_id,type,embedding_model_id,vector_store_id,indexing_strategy,
chunking_config,faq_config`).Where("id=? AND tenant_id=? AND deleted_at IS NULL", kbID, tenant).Scan(kb).Error
	if err != nil {
		return nil, err
	}
	if kb.ID != kbID {
		return nil, errPersonalExactDocument
	}
	kb.PrivateOwnerID = owner.PrivateOwnerID
	return kb, nil
}
