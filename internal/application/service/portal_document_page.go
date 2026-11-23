package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// ResolveDocument narrows only independently issued live grants. No client KB
// claim or unscoped body lookup creates authority for the requested document.
func (s *PortalMCPKnowledge) ResolveDocument(ctx context.Context, id string) ([]*access.PortalKBTarget, error) {
	if strings.TrimSpace(id) == "" {
		return nil, access.ErrForbidden
	}
	var ids []string
	if s == nil || s.Resolver == nil {
		return nil, access.ErrForbidden
	}
	if directory, ok := s.Resolver.Directory.(interface {
		LookupPortalDocumentKB(context.Context, string) (string, error)
	}); ok {
		kb, err := directory.LookupPortalDocumentKB(ctx, id)
		if err != nil {
			return nil, err
		}
		if kb == "" {
			return nil, access.ErrForbidden
		}
		ids = []string{kb}
	}
	targets, err := s.Resolve(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if portalContains(target, id) {
			return []*access.PortalKBTarget{target}, nil
		}
	}
	return nil, access.ErrForbidden
}

type readableTextPager interface {
	ListReadableTextPage(context.Context, uint64, string, string, int, int) ([]*types.Chunk, int64, error)
}

// ReadDocumentPage never materializes the entire document. The existing live
// KB, publication, source and output checks still bracket each bounded page.
func (s *PortalMCPKnowledge) ReadDocumentPage(
	ctx context.Context, id string, offset, limit int,
) ([]PortalMCPHit, int64, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, access.ErrForbidden
	}
	targets, err := s.ResolveDocument(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	pager, ok := s.Chunks.(readableTextPager)
	if !ok {
		// Existing adapters keep their original 1,000-chunk safety bound.
		// SQL repositories implement the bounded page capability above.
		rows, err := s.ReadDocument(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		total := len(rows)
		if offset > total {
			offset = total
		}
		end := offset + min(limit, total-offset)
		return rows[offset:end], int64(total), nil
	}
	target := targets[0]
	knowledge, err := s.document(ctx, target, id)
	if err != nil {
		return nil, 0, err
	}
	read, err := s.publishedDocumentContext(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	d := target.Descriptor()
	chunks, total, err := pager.ListReadableTextPage(read, d.TenantID, id, d.ID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	if total < 0 || len(chunks) > limit {
		return nil, 0, access.ErrForbidden
	}
	out := make([]PortalMCPHit, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || chunk.KnowledgeID != id || chunk.KnowledgeBaseID != d.ID ||
			chunk.TenantID != d.TenantID || chunk.DeletedAt.Valid || !chunk.IsEnabled ||
			chunk.ChunkType != types.ChunkTypeText {
			return nil, 0, access.ErrForbidden
		}
		text, err := access.SanitizePortalText(chunk.Content)
		if err != nil {
			return nil, 0, err
		}
		selected := *chunk
		selected.Content = text
		out = append(out, portalHit(target, knowledge, &selected))
	}
	if err := validatePortalTargets(ctx, targets); err != nil {
		return nil, 0, err
	}
	if err := s.validatePublishedPortalHits(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
