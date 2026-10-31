package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) exactPublicReadContext(ctx context.Context, id string) (context.Context, bool, error) {
	if !exactPersonalRecoveryEnabled() || s.DocumentACLReadEnabled() {
		return ctx, false, nil
	}
	if _, _, _, _, ok := types.ExactPublishedDocumentIdentity(ctx); ok {
		_, err := types.CheckExactPublishedDocumentRead(ctx)
		return ctx, true, err
	}
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok {
		return ctx, false, nil
	}
	var count int64
	if err := p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).Where(
		"scope=? AND scope_id=?", types.PersonalExactReceiptScope, id).Count(&count).Error; err != nil {
		return ctx, false, err
	}
	if count == 0 {
		return ctx, false, nil
	}
	guarded, err := p.exactPublishedReadContext(ctx, id)
	return guarded, true, err
}

// Enumerate metadata only, then fetch each body with its independent proof.
// Ordinary document reads keep their existing policy; denied/unknown content
// is never loaded and then post-filtered. The whole response fails closed.
func (s *knowledgeService) exactPublicKnowledgePage(ctx context.Context, kb string,
	page *types.Pagination, filter types.KnowledgeListFilter,
) (*types.PageResult, bool, error) {
	if !exactPersonalRecoveryEnabled() || s.DocumentACLReadEnabled() {
		return nil, false, nil
	}
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok {
		return nil, false, nil
	}
	var count int64
	if err := p.service.db.WithContext(ctx).Table("task_pending_ops").Where(
		"scope=? AND payload->>'knowledge_base_id'=?", types.PersonalExactReceiptScope, kb).
		Count(&count).Error; err != nil {
		return nil, true, err
	}
	if count == 0 {
		return nil, false, nil
	}
	owner, err := p.loadAuthorizationKB(ctx, kb)
	if err != nil {
		return nil, true, err
	}
	tenant, valid := types.TenantIDFromContext(ctx)
	if !valid || tenant != owner.TenantID {
		return nil, true, access.ErrForbidden
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		owner, types.OrgRoleViewer, p.shares, nil)
	if err != nil {
		return nil, true, err
	}
	ctx = grant.Context(ctx)
	catalog, ok := s.repo.(interface {
		ListKnowledgePageIdentities(context.Context, uint64, string, *types.Pagination,
			types.KnowledgeListFilter) ([]string, int64, error)
	})
	if !ok {
		return nil, true, errPersonalExactDocument
	}
	ids, total, err := catalog.ListKnowledgePageIdentities(ctx, tenant, kb, page, filter)
	if err != nil {
		return nil, true, err
	}
	rows := make([]*types.Knowledge, 0, len(ids))
	reads := make([]context.Context, 0, len(ids))
	for _, id := range ids {
		read, exact, err := s.exactPublicReadContext(ctx, id)
		if err != nil {
			return nil, true, err
		}
		row, err := s.repo.GetKnowledgeByID(read, tenant, id)
		if err != nil || row == nil || row.KnowledgeBaseID != kb {
			if err == nil {
				err = errPersonalExactDocument
			}
			return nil, true, err
		}
		// Tags are separate historical rows and are not blessed by a file receipt.
		if exact {
			reads = append(reads, read)
		}
		rows = append(rows, row)
	}
	for _, read := range reads {
		if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
			return nil, true, err
		}
	}
	return types.NewPageResult(total, page, rows), true, nil
}
