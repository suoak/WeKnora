package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

var errFileProcessingEnqueueUnknown = errors.New("processing_enqueue_outcome_unknown")

func (s *knowledgeService) createFileProcessingKnowledge(ctx context.Context, knowledge *types.Knowledge) (int64, error) {
	if s.config == nil || s.config.KnowledgeBase == nil || !s.config.KnowledgeBase.FencedFileAdmission {
		return 0, s.repo.CreateKnowledge(ctx, knowledge)
	}
	provider, ok := s.repo.(interface {
		CreateKnowledgeForProcessing(context.Context, *types.Knowledge) (int64, error)
	})
	if !ok {
		return 0, errProcessingExecution
	}
	return provider.CreateKnowledgeForProcessing(ctx, knowledge)
}

func (s *knowledgeService) markFileProcessingEnqueueFailed(ctx context.Context, knowledge *types.Knowledge, generation int64) {
	if generation == 0 {
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		return
	}
	provider, ok := s.repo.(interface {
		ProcessingFences() *repository.ProcessingFenceRepository
	})
	if !ok {
		return
	}
	// Queue errors may be ambiguous: a concurrently claimed delivery remains
	// authoritative. This conditional write cannot replace it or a newer attempt.
	if err := provider.ProcessingFences().FailPendingAdmission(ctx, knowledge.TenantID, knowledge.ID, generation); err != nil {
		logger.Warnf(ctx, "File admission status unchanged, knowledge_id=%s, code=admission_not_current", knowledge.ID)
		return
	}
	knowledge.ParseStatus = types.ParseStatusFailed
	knowledge.ErrorMessage = "Failed to enqueue processing task"
}
