package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

func (s *wikiIngestService) wikiBlocked(ctx context.Context, kb string) error {
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return nil
	}
	block, err := repository.ReadWikiProcessingBlock(ctx, pages.repo, kb)
	if err != nil {
		return err
	}
	if block != nil && block.Reason != "" && time.Now().Before(block.RetryAfter) {
		return fmt.Errorf("wiki %s; durable operations retained: %w", block.Reason, asynq.SkipRetry)
	}
	return nil
}

func (s *wikiIngestService) recordWikiBlock(ctx context.Context, payload WikiIngestPayload, result *error) {
	if *result == nil || errors.Is(*result, asynq.SkipRetry) {
		return
	}
	reason := ""
	switch {
	case errors.Is(*result, types.ErrLegacySourceRestricted),
		errors.Is(*result, types.ErrPrivateKnowledgeBase), errors.Is(*result, types.ErrExactDocumentScope):
		reason = "source_verification_required"
	case errors.Is(*result, repository.ErrWikiMCPUnverified):
		reason = "verified_regeneration_required"
	default:
		return
	}
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return
	}
	cleanup, cancel := wikiIngestCleanupContext(ctx)
	defer cancel()
	if err := repository.WriteWikiProcessingBlock(
		cleanup, pages.repo, payload.TenantID, payload.KnowledgeBaseID, reason,
	); err != nil {
		*result = errors.Join(*result, fmt.Errorf("persist wiki block: %w", err))
		return
	}
	// Asynq archives this trigger with its original identity and error. The SQL
	// operations remain pending; housekeeping can recheck authority after cooldown.
	*result = errors.Join(*result, asynq.SkipRetry)
}

func (s *wikiIngestService) clearWikiBlock(ctx context.Context, payload WikiIngestPayload) error {
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return nil
	}
	block, err := repository.ReadWikiProcessingBlock(ctx, pages.repo, payload.KnowledgeBaseID)
	if err != nil || block == nil || block.Reason == "" {
		return err
	}
	return repository.WriteWikiProcessingBlock(ctx, pages.repo, payload.TenantID, payload.KnowledgeBaseID, "")
}
