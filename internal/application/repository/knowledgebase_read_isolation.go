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
