package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

func (s *wikiIngestService) generationKnowledge(ctx context.Context, id string) (*types.Knowledge, error) {
	if knowledge, ok := s.knowledgeSvc.(*knowledgeService); ok {
		if reader, ok := knowledge.repo.(interface {
			ReadWikiGenerationKnowledge(context.Context, string) (*types.Knowledge, bool, error)
		}); ok {
			row, handled, err := reader.ReadWikiGenerationKnowledge(ctx, id)
			if handled || err != nil {
				return row, err
			}
		}
	}
	return s.knowledgeSvc.GetKnowledgeByIDOnly(ctx, id)
}
