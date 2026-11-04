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
		readCtx := ctx
		if types.KnowledgeBaseCatalogTenant(ctx) != 0 {
			readCtx = types.WithLegacyReadScope(ctx)
		}
		kb, e := r.GetKnowledgeBaseByID(readCtx, id)
		if errors.Is(e, types.ErrPrivateKnowledgeBase) || errors.Is(e, ErrKnowledgeBaseNotFound) {
			continue
		}
		// Catalog visibility is independent of source-content authorization.
		// Retry only a source rejection, only in the catalog's tenant, and
		// strip generated content. All other callers keep strict reads.
		if errors.Is(e, types.ErrLegacySourceRestricted) && types.KnowledgeBaseCatalogTenant(ctx) != 0 {
			// The failed attempt released no body. Do not let its private-KB
			// source marker poison the entire catalog's final response check.
			readCtx = types.WithLegacyReadScope(ctx)
			kb, e = r.GetKnowledgeBaseByID(types.WithLegacyKBAdminMetadata(readCtx, id), id)
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
		types.MergeLegacyReadScope(ctx, readCtx)
		out = append(out, kb)
	}
	return out, nil
}
