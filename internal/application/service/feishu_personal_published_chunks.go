package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Only the owning Knowledge ID is selected before authorization, never content.
func (s *chunkService) exactPublishedChunkContext(
	ctx context.Context,
	knowledge, chunk string,
) (context.Context, string, bool, error) {
	if s.personalRecovery == nil {
		_, _, target, _, exact := types.ExactPublishedDocumentIdentity(ctx)
		if exact && (knowledge == "" || knowledge == target) {
			_, err := types.CheckExactPublishedDocumentRead(ctx)
			return ctx, target, true, err
		}
		return ctx, knowledge, false, nil
	}
	if knowledge == "" {
		var metadata struct{ KnowledgeID string }
		if err := s.personalRecovery.service.db.WithContext(ctx).Table("chunks").Select("knowledge_id").
			Where("id=? AND deleted_at IS NULL", chunk).Take(&metadata).Error; err != nil {
			return ctx, "", false, err
		}
		knowledge = metadata.KnowledgeID
	}
	ks := s.personalRecovery.service.knowledge.(*knowledgeService)
	read, handled, err := ks.exactPublicReadContext(ctx, knowledge)
	return read, knowledge, handled, err
}
