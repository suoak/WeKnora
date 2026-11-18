package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// ReadWikiGenerationKnowledge uses only a server-issued generation selection.
// It does not issue an interactive viewer grant or widen the selected inputs.
func (r *knowledgeRepository) ReadWikiGenerationKnowledge(
	ctx context.Context, id string,
) (*types.Knowledge, bool, error) {
	selection, selected := ctx.Value(wikiDocumentSelectionKey{}).(*WikiDocumentSelection)
	proof, proved := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !selected || !proved {
		return nil, false, nil
	}
	if !selection.Eligible[id] || (!WikiDocumentSelectionForKB(proof.KB) && proof.Replacement == nil) {
		return nil, true, ErrWikiMCPUnverified
	}
	if tenant, ok := types.TenantIDFromContext(ctx); ok && tenant != proof.Tenant {
		return nil, true, ErrWikiMCPUnverified
	}
	check := func() error {
		guarded, err := types.CheckWikiVerifiedRead(r.db.WithContext(ctx), id, proof.KB, nil)
		if err != nil {
			return err
		}
		if !guarded {
			return ErrWikiMCPUnverified
		}
		return nil
	}
	if err := check(); err != nil {
		return nil, true, err
	}
	row, err := r.GetKnowledgeByID(ctx, proof.Tenant, id)
	if err != nil {
		return nil, true, err
	}
	if row.KnowledgeBaseID != proof.KB || row.TenantID != proof.Tenant {
		return nil, true, ErrWikiMCPUnverified
	}
	if err := check(); err != nil {
		return nil, true, err
	}
	return row, true, nil
}
