package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DocumentReadPolicy connects the existing ACL to actual business services.
// The container supplies disabled policy until business verifiers and remaining
// read surfaces are certified. Tests inject an enabled, synthetic verifier.
type DocumentReadPolicy struct {
	enabled bool
	reader  *access.DocumentReader
}

func NewDocumentReadPolicy(enabled bool, reader *access.DocumentReader) *DocumentReadPolicy {
	return &DocumentReadPolicy{enabled: enabled, reader: reader}
}
func ConfigureKnowledgeDocumentReads(s interfaces.KnowledgeService, p *DocumentReadPolicy) (interfaces.KnowledgeService, error) {
	concrete, ok := s.(*knowledgeService)
	if !ok {
		return nil, access.ErrForbidden
	}
	concrete.documentACL = p
	return s, nil
}
func ConfigureChunkDocumentReads(s interfaces.ChunkService, p *DocumentReadPolicy) (interfaces.ChunkService, error) {
	concrete, ok := s.(*chunkService)
	if !ok {
		return nil, access.ErrForbidden
	}
	concrete.documentACL = p
	return s, nil
}
func (s *knowledgeService) DocumentACLReadEnabled() bool {
	return s.documentACL != nil && s.documentACL.enabled
}
func (s *chunkService) DocumentACLReadEnabled() bool {
	return s.documentACL != nil && s.documentACL.enabled
}

// KnowledgeDocumentReadService is a server-side extension, never a client
// header. It conveys no generic KB privilege or space membership.
type KnowledgeDocumentReadService interface {
	DocumentACLReadEnabled() bool
	DocumentKBReadContext(context.Context, string) (context.Context, error)
}

func (s *knowledgeService) DocumentKBReadContext(ctx context.Context, kb string) (context.Context, error) {
	if !s.DocumentACLReadEnabled() {
		return nil, access.ErrForbidden
	}
	scope, err := s.documentACL.reader.AuthorizeKBReads(ctx, kb)
	if err != nil {
		return nil, err
	}
	guarded := scope.Context(types.WithExecutionTenant(ctx, scope.TenantID()))
	if err = scope.RetainForOutput(guarded); err != nil {
		return nil, err
	}
	return guarded, nil
}
func authorizeStoredDocument(ctx context.Context, p *DocumentReadPolicy, repo interfaces.KnowledgeRepository, id string) (*types.Knowledge, *access.DocumentReadGrant, error) {
	if p == nil || !p.enabled || p.reader == nil || repo == nil || id == "" {
		return nil, nil, access.ErrForbidden
	}
	// This repository is private to the service; the persisted owner is used,
	// never a client-supplied KB/tenant. Nothing is emitted before authorization.
	row, err := repo.GetKnowledgeByIDOnly(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.ID != id || row.TenantID == 0 || row.KnowledgeBaseID == "" {
		return nil, nil, access.ErrForbidden
	}
	grant, err := p.reader.Authorize(ctx, row.KnowledgeBaseID, id, types.DocumentRead)
	if err != nil {
		return nil, nil, err
	}
	fresh, err := repo.GetKnowledgeByID(ctx, row.TenantID, id)
	if err != nil {
		return nil, nil, err
	}
	if fresh == nil || fresh.ID != id || fresh.TenantID != row.TenantID || fresh.KnowledgeBaseID != row.KnowledgeBaseID {
		return nil, nil, access.ErrForbidden
	}
	if err = grant.Revalidate(ctx); err != nil {
		return nil, nil, err
	}
	return fresh, grant, nil
}
func protectedKnowledgeProjection(row *types.Knowledge) (*types.Knowledge, error) {
	if row == nil || !access.SafeDocumentText(row.Title) || !access.SafeDocumentText(row.FileName) || !access.SafeDocumentText(row.FolderPath) {
		return nil, access.ErrForbidden
	}
	// File/storage URLs, arbitrary metadata, generated profiles and historical
	// summaries require their own resource/provenance proof and remain HOLD.
	return &types.Knowledge{ID: row.ID, TenantID: row.TenantID, KnowledgeBaseID: row.KnowledgeBaseID, Type: row.Type, Title: row.Title, FileName: row.FileName, FolderPath: row.FolderPath, FileType: row.FileType, ParseStatus: row.ParseStatus, EnableStatus: row.EnableStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}
func (s *knowledgeService) readAuthorizedKnowledgeWithGrant(ctx context.Context, id string) (*types.Knowledge, *access.DocumentReadGrant, error) {
	row, grant, err := authorizeStoredDocument(ctx, s.documentACL, s.repo, id)
	if err != nil {
		return nil, nil, err
	}
	result, err := protectedKnowledgeProjection(row)
	if err != nil {
		return nil, nil, err
	}
	// Tags are held until tag/relation writes participate in the content epoch.
	if err = grant.RetainForOutput(ctx); err != nil {
		return nil, nil, err
	}
	return result, grant, nil
}
func (s *knowledgeService) readAuthorizedKnowledge(ctx context.Context, id string) (*types.Knowledge, error) {
	row, _, err := s.readAuthorizedKnowledgeWithGrant(ctx, id)
	return row, err
}
func (s *knowledgeService) readAuthorizedPage(ctx context.Context, kb string, page *types.Pagination, filter types.KnowledgeListFilter) (*types.PageResult, error) {
	if len(filter.TagIDs) > 0 {
		return nil, access.ErrForbidden
	} // Tag/relation epoch coverage is HOLD.
	if page == nil {
		return nil, access.ErrForbidden
	}
	guarded, err := s.DocumentKBReadContext(ctx, kb)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.ListPagedKnowledgeByKnowledgeBaseID(guarded, types.MustTenantIDFromContext(guarded), kb, page, filter)
	if err != nil {
		return nil, err
	}
	result := make([]*types.Knowledge, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.KnowledgeBaseID != kb || row.TenantID != types.MustTenantIDFromContext(guarded) {
			return nil, access.ErrForbidden
		}
		projected, err := s.readAuthorizedKnowledge(guarded, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, projected)
	}
	if _, _, err = access.GuardDocumentReadQuery(guarded, types.MustTenantIDFromContext(guarded), kb); err != nil {
		return nil, err
	}
	return types.NewPageResult(total, page, result), nil
}
func (s *knowledgeService) readAuthorizedBatch(ctx context.Context, ids []string) ([]*types.Knowledge, error) {
	if len(ids) > access.MaxDocumentAllowSet {
		return nil, access.ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || p.Type != types.PrincipalWebUser || p.ID == "" || types.CallerFromContext(ctx).UserID != p.ID {
		return nil, access.ErrUnauthorized
	}
	out := make([]*types.Knowledge, 0, len(ids))
	leases := []*access.DocumentReadGrant{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, access.ErrForbidden
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		row, grant, err := s.readAuthorizedKnowledgeWithGrant(ctx, id)
		if errors.Is(err, access.ErrForbidden) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, row)
		leases = append(leases, grant)
	}
	for _, grant := range leases {
		if err := grant.RetainForOutput(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *chunkService) readAuthorizedChunkWithGrant(ctx context.Context, id string) (*types.Chunk, *access.DocumentReadGrant, error) {
	row, err := s.chunkRepository.GetChunkByIDOnly(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.ID != id {
		return nil, nil, access.ErrForbidden
	}
	doc, grant, err := authorizeStoredDocument(ctx, s.documentACL, s.knowledgeRepo, row.KnowledgeID)
	if err != nil {
		return nil, nil, err
	}
	row, err = s.chunkRepository.GetChunkByID(ctx, doc.TenantID, id)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.ID != id || row.KnowledgeID != doc.ID {
		return nil, nil, access.ErrForbidden
	}
	if row.TenantID != doc.TenantID || row.KnowledgeBaseID != doc.KnowledgeBaseID || row.DeletedAt.Valid || !row.IsEnabled || row.ChunkType != types.ChunkTypeText || !access.SafeDocumentText(row.Content) {
		return nil, nil, access.ErrForbidden
	}
	result := &types.Chunk{ID: row.ID, TenantID: row.TenantID, KnowledgeBaseID: row.KnowledgeBaseID, KnowledgeID: row.KnowledgeID, Content: row.Content, ChunkType: row.ChunkType, ChunkIndex: row.ChunkIndex, IsEnabled: row.IsEnabled, ContentRevision: row.ContentRevision}
	if err = grant.RetainForOutput(ctx); err != nil {
		return nil, nil, err
	}
	return result, grant, nil
}
func (s *chunkService) readAuthorizedChunk(ctx context.Context, id string) (*types.Chunk, error) {
	row, _, err := s.readAuthorizedChunkWithGrant(ctx, id)
	return row, err
}
func (s *chunkService) readAuthorizedChunks(ctx context.Context, id string) ([]*types.Chunk, error) {
	doc, grant, err := authorizeStoredDocument(ctx, s.documentACL, s.knowledgeRepo, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.chunkRepository.ListChunksByKnowledgeID(ctx, doc.TenantID, id)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Chunk, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.KnowledgeID != id || row.KnowledgeBaseID != doc.KnowledgeBaseID || row.TenantID != doc.TenantID {
			return nil, access.ErrForbidden
		}
		if !row.IsEnabled {
			continue
		}
		current, err := s.readAuthorizedChunk(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, current)
	}
	if err = grant.RetainForOutput(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *chunkService) readAuthorizedChunkPage(ctx context.Context, id string, page *types.Pagination, kinds []types.ChunkType) (*types.PageResult, error) {
	if page == nil {
		return nil, access.ErrForbidden
	}
	for _, kind := range kinds {
		if kind != types.ChunkTypeText {
			return nil, access.ErrForbidden
		}
	}
	rows, err := s.readAuthorizedChunks(ctx, id)
	if err != nil {
		return nil, err
	}
	offset, limit := page.Offset(), page.Limit()
	if offset < 0 || limit < 1 {
		return nil, access.ErrForbidden
	}
	total := len(rows)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return types.NewPageResult(int64(total), page, rows[offset:end]), nil
}
func (s *chunkService) readAuthorizedChildren(ctx context.Context, tenant uint64, parentID string) ([]*types.Chunk, error) {
	parent, err := s.chunkRepository.GetChunkByIDOnly(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.TenantID != tenant {
		return nil, access.ErrForbidden
	}
	doc, grant, err := authorizeStoredDocument(ctx, s.documentACL, s.knowledgeRepo, parent.KnowledgeID)
	if err != nil {
		return nil, err
	}
	if parent.KnowledgeBaseID != doc.KnowledgeBaseID || parent.TenantID != doc.TenantID {
		return nil, access.ErrForbidden
	}
	rows, err := s.chunkRepository.ListChunkByParentID(ctx, tenant, parentID)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Chunk, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.ParentChunkID != parentID || row.KnowledgeID != doc.ID || row.KnowledgeBaseID != doc.KnowledgeBaseID || row.TenantID != doc.TenantID {
			return nil, access.ErrForbidden
		}
		if !row.IsEnabled {
			continue
		}
		safe, err := s.readAuthorizedChunk(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, safe)
	}
	if err = grant.RetainForOutput(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func ConfigureKnowledgeBaseDocumentReads(s interfaces.KnowledgeBaseService, p *DocumentReadPolicy) (interfaces.KnowledgeBaseService, error) {
	concrete, ok := s.(*knowledgeBaseService)
	if !ok {
		return nil, access.ErrForbidden
	}
	concrete.documentACL = p
	return s, nil
}
