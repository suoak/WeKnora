package service

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/hibiken/asynq"
)

func (s *wikiIngestService) withGenerationLease(
	ctx context.Context, task *asynq.Task, fn func(context.Context, *asynq.Task) error,
) error {
	if !s.personalWikiProofEnabled() {
		return fn(ctx, task)
	}
	var payload WikiIngestPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return wikilock.With(ctx, s.redisClient, payload.KnowledgeBaseID, func(locked context.Context) error {
		return fn(locked, task)
	})
}

// ProcessWikiIngest holds the same lease as finalize and historical reset.
func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, task *asynq.Task) error {
	return s.withGenerationLease(ctx, task, s.processWikiIngest)
}

// ProcessWikiFinalize cannot invalidate another batch's generation proof.
func (s *wikiIngestService) ProcessWikiFinalize(ctx context.Context, task *asynq.Task) error {
	return s.withGenerationLease(ctx, task, s.processWikiFinalize)
}
