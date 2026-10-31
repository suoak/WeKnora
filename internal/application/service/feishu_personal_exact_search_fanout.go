package service

import (
	"context"
	"errors"
	"sort"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type exactSearchFanoutBypassKey struct{}

// Each recovered file is searched through the ordinary pipeline with its own
// proof. Combining results never combines those proofs into a KB-wide grant.
// Unreceipted healthy documents retain the ordinary pipeline and source gate.
func (s *knowledgeBaseService) exactRecoveryHybridSearch(
	ctx context.Context, kbID string, params types.SearchParams,
) (bool, []*types.SearchResult, error) {
	if !exactPersonalRecoveryEnabled() || s.personalRecovery == nil || len(params.KnowledgeIDs) == 1 ||
		ctx.Value(exactSearchFanoutBypassKey{}) != nil {
		return false, nil, nil
	}
	if _, _, _, _, exact := types.ExactPublishedDocumentIdentity(ctx); exact {
		return false, nil, nil
	}
	if len(params.KnowledgeBaseIDs) != 0 &&
		(len(params.KnowledgeBaseIDs) != 1 || params.KnowledgeBaseIDs[0] != kbID) {
		return false, nil, nil // Multi-KB requests keep their existing strict path.
	}
	p := s.personalRecovery
	// Do not enumerate another user's private KB or its receipt metadata.
	kb, err := p.loadAuthorizationKB(ctx, kbID)
	if err != nil {
		return true, nil, err
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		kb, types.OrgRoleViewer, p.shares, nil)
	if err != nil {
		return true, nil, err
	}
	read := types.WithExecutionTenant(grant.Context(ctx), kb.TenantID)
	query := p.service.db.WithContext(read).Table("task_pending_ops").Where(
		"scope=? AND tenant_id=? AND payload->>'knowledge_base_id'=?",
		types.PersonalExactReceiptScope, kb.TenantID, kbID,
	)
	if len(params.KnowledgeIDs) != 0 {
		query = query.Where("scope_id IN ?", params.KnowledgeIDs)
	}
	var ids []string
	if err := query.Order("id").Limit(257).Pluck("scope_id", &ids).Error; err != nil {
		return true, nil, err
	}
	if len(ids) == 0 {
		return false, nil, nil
	}
	if len(ids) > 256 {
		return true, nil, errors.New("exact_document_search_scope_requires_narrowing")
	}
	params.MatchCount = normalizedMatchCount(params.MatchCount)
	ordinary := context.WithValue(ctx, exactSearchFanoutBypassKey{}, true)
	var combined []*types.SearchResult
	if err := types.CheckLegacySourceRead(p.service.db.WithContext(read), "", kbID); err == nil {
		results, err := s.HybridSearch(ordinary, kbID, params)
		if err != nil {
			return true, nil, err
		}
		combined = append(combined, results...)
	} else if !errors.Is(err, types.ErrLegacySourceRestricted) {
		return true, nil, err
	}
	verified := make(map[string]context.Context)
	for _, id := range ids {
		scope, err := p.exactPublishedReadContext(ctx, id)
		if err != nil {
			if errors.Is(err, errPersonalExactDocument) || personalAuthorizationLost(err) ||
				errors.Is(err, types.ErrLegacySourceRestricted) {
				continue // An invalid file must not authorize or block other files.
			}
			return true, nil, err
		}
		target := params
		target.KnowledgeIDs = []string{id}
		results, err := s.HybridSearch(scope, kbID, target)
		if err != nil {
			return true, nil, err
		}
		verified[id] = scope
		combined = append(combined, results...)
	}
	// Revalidate every released recovered document after the complete fan-out.
	// Any revocation during another document's query discards the response.
	for _, scope := range verified {
		if _, err := types.CheckExactPublishedDocumentRead(scope); err != nil {
			return true, nil, err
		}
	}
	sort.SliceStable(combined, func(i, j int) bool { return combined[i].Score > combined[j].Score })
	out := make([]*types.SearchResult, 0, min(len(combined), params.MatchCount))
	seen := make(map[string]bool)
	for _, result := range combined {
		if seen[result.ID] {
			continue
		}
		seen[result.ID] = true
		out = append(out, result)
		if len(out) == params.MatchCount {
			break
		}
	}
	return true, out, nil
}
