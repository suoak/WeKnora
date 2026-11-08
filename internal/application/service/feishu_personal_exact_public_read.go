package service

import (
	"bytes"
	"context"
	"io"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type exactPublishedFileReader struct{ *bytes.Reader }

func (*exactPublishedFileReader) Close() error { return nil }

func (s *knowledgeService) exactPublicKnowledgeFile(
	ctx context.Context,
	id string,
) (io.ReadCloser, string, bool, error) {
	read := ctx
	_, _, knowledge, _, exact := types.ExactPublishedDocumentIdentity(ctx)
	if exact && knowledge != id {
		return nil, "", true, types.ErrExactDocumentScope
	}
	if !exact {
		var err error
		read, exact, err = s.exactPublicReadContext(ctx, id)
		if err != nil || !exact {
			return nil, "", exact, err
		}
	}
	// This full live check includes the current KnowHub viewer, local published
	// version, file/manifest SHA256, unique binding and published DB receipt.
	// It refreshes the immutable bytes below; no path is reopened afterwards.
	if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
		return nil, "", true, err
	}
	file, ok := read.Value(exactPublishedFileKey{}).(*exactPublishedFileSnapshot)
	if !ok || file == nil || file.knowledge != id {
		return nil, "", true, types.ErrExactDocumentScope
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	if len(file.data) == 0 || file.filename == "" {
		return nil, "", true, types.ErrExactDocumentScope
	}
	return &exactPublishedFileReader{bytes.NewReader(file.data)}, file.filename, true, nil
}

// ExactPublishedReadContext issues source authority only for this document and
// the authenticated viewer. Route guards must retain it for downstream reads.
func (s *knowledgeService) ExactPublishedReadContext(ctx context.Context, id string) (context.Context, bool, error) {
	return s.exactPublicReadContext(ctx, id)
}

// Keep the single-row read inside the same checked SQL boundary as chunk pages.
// An existing route capability is an identity, not a cached authorization:
// ReadExactPublishedPage still performs full live validation before selection
// and again before returning any content. Row hooks retain the exact target.
func (s *knowledgeService) readExactPublicKnowledge(ctx context.Context, id string) (*types.Knowledge, bool, error) {
	read := ctx
	_, tenant, knowledge, _, exact := types.ExactPublishedDocumentIdentity(ctx)
	if exact && knowledge != id {
		return nil, true, types.ErrExactDocumentScope
	}
	if !exact {
		var err error
		read, exact, err = s.exactPublicReadContext(ctx, id)
		if err != nil || !exact {
			return nil, exact, err
		}
		_, tenant, _, _, _ = types.ExactPublishedDocumentIdentity(read)
	}
	var row *types.Knowledge
	err := types.ReadExactPublishedPage(read, id, func(selection context.Context) error {
		var err error
		row, err = s.repo.GetKnowledgeByID(selection, tenant, id)
		return err
	})
	if err != nil {
		return nil, true, err
	}
	return row, true, nil
}

func (s *knowledgeService) exactPublicReadContext(ctx context.Context, id string) (context.Context, bool, error) {
	if _, _, knowledge, _, ok := types.ExactPublishedDocumentIdentity(ctx); ok {
		if knowledge != id {
			return ctx, true, types.ErrExactDocumentScope
		}
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
		var personal bool
		if err := p.service.db.WithContext(ctx).
			Raw("SELECT EXISTS(SELECT 1 FROM knowledges WHERE id=? AND channel='feishu_personal')", id).
			Scan(&personal).Error; err != nil {
			return ctx, true, err
		}
		if personal {
			var kb string
			if err := p.service.db.WithContext(ctx).Table("knowledges").Select("knowledge_base_id").Where("id=?", id).Scan(&kb).Error; err != nil {
				return ctx, true, err
			}
			handedOff, err := types.VerifiedSourceHandoff(p.service.db.WithContext(ctx), id, kb)
			if err != nil {
				return ctx, true, err
			}
			if !handedOff {
				return ctx, true, errPersonalExactDocument
			}
			// The ordinary repository still enforces private owner, ACL and source revocation.
		}
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
	if _, err := p.checkExactPublishedViewer(ctx, types.PersonalExactReceipt{
		TenantID: owner.TenantID, KnowledgeBaseID: owner.ID,
	}); err != nil {
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
	if types.CallerFromContext(ctx).TenantID == tenant {
		rows, err := p.exactPageMetadata(ctx, tenant, kb, ids)
		if err != nil {
			return nil, true, err
		}
		return types.NewPageResult(total, page, rows), true, nil
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
