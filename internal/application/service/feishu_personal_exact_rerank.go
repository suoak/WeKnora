package service

import (
	"context"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/reranking"
	"github.com/Tencent/WeKnora/internal/types"
)

// Only the normal retrieval builder can produce this adapter. Actual passages
// are compared with canonical, receipted database rows, including the title,
// context header, generated questions and image-derived text used by ModelPassage.
type exactPublishedReranker struct {
	rerank.Reranker
	query    string
	passages []string
	tenant   uint64
}

func (m *exactPublishedReranker) MaxPassageRunes(query string) int {
	return rerank.MaxPassageRunes(m.Reranker, query)
}

func (m *exactPublishedReranker) Rerank(
	ctx context.Context, query string, passages []string,
) ([]rerank.RankResult, error) {
	if query != m.query || !slices.Equal(passages, m.passages) {
		return nil, errPersonalExactDocument
	}
	live := func(current context.Context) error {
		present, err := types.CheckExactPublishedDocumentRead(current)
		if err != nil {
			return err
		}
		if !present {
			return errPersonalExactDocument
		}
		return nil
	}
	work, err := withExactPersonalPassages(ctx, m.tenant, query, passages, live)
	if err != nil {
		return nil, err
	}
	results, err := m.Reranker.Rerank(work, query, passages)
	if err != nil {
		return nil, err
	}
	if err = live(work); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *knowledgeBaseService) exactRerankModel(
	ctx context.Context, model rerank.Reranker, query string, candidates []*types.SearchResult,
) (rerank.Reranker, error) {
	_, tenant, knowledge, kb, exact := types.ExactPublishedDocumentIdentity(ctx)
	if !exact {
		return model, nil
	}
	if _, err := types.CheckExactPublishedDocumentRead(ctx); err != nil {
		return nil, err
	}
	owner, err := s.kgRepo.GetKnowledgeByID(ctx, tenant, knowledge)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.KnowledgeID != knowledge || candidate.KnowledgeBaseID != kb {
			return nil, errPersonalExactDocument
		}
		ids = append(ids, candidate.ID)
	}
	rows, err := s.chunkRepo.ListChunksByID(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*types.Chunk, len(rows))
	for _, row := range rows {
		if row.KnowledgeID != knowledge || row.KnowledgeBaseID != kb || !row.IsEnabled {
			return nil, errPersonalExactDocument
		}
		byID[row.ID] = row
	}
	var passages []string
	limit := rerank.MaxPassageRunes(model, query)
	for _, candidate := range candidates {
		row := byID[candidate.ID]
		if row == nil {
			return nil, errPersonalExactDocument
		}
		canonical := s.buildSearchResult(row, owner, candidate.Score, candidate.MatchType, "")
		passage := reranking.ModelPassage(ctx, canonical)
		if passage != reranking.ModelPassage(ctx, candidate) {
			return nil, errPersonalExactDocument
		}
		if strings.TrimSpace(passage) == "" {
			continue
		}
		if limit > 0 && len([]rune(passage)) > limit {
			passage = string([]rune(passage)[:limit])
		}
		passages = append(passages, passage)
	}
	if len(passages) == 0 {
		return nil, errPersonalExactDocument
	}
	return &exactPublishedReranker{Reranker: model, query: query, passages: passages, tenant: tenant}, nil
}
