package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// Read-only fan-out; a source rejection cannot poison unrelated KBs. The
// caller still authorizes each remaining KB. Storage and identity errors are
// never treated as a healthy empty result. Direct single-KB reads stay strict.
func (r *knowledgeBaseRepository) GetReadKnowledgeBasesByIDs(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	out := []*types.KnowledgeBase{}
	for _, id := range ids {
		kb, e := r.GetKnowledgeBaseByID(ctx, id)
		if errors.Is(e, types.ErrPrivateKnowledgeBase) || errors.Is(e, ErrKnowledgeBaseNotFound) {
			continue
		}
		// Catalog visibility is independent of source-content authorization.
		// Retry only a source rejection, only in the catalog's tenant, and
		// strip generated content. All other callers keep strict reads.
		if errors.Is(e, types.ErrLegacySourceRestricted) && types.KnowledgeBaseCatalogTenant(ctx) != 0 {
			kb, e = r.GetKnowledgeBaseByID(types.WithLegacyKBAdminMetadata(ctx, id), id)
			if e == nil {
				if kb.TenantID != types.KnowledgeBaseCatalogTenant(ctx) {
					continue
				}
				kb.GeneratedProfile = nil
			}
		}
		if errors.Is(e, types.ErrLegacySourceRestricted) {
			types.RecordLegacyReadExclusion(ctx)
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, kb)
	}
	return out, nil
}
