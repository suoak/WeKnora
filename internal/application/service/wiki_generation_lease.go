package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
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
	return withWorkerWikiGenerationLease(ctx, s.redisClient, payload.KnowledgeBaseID, func(locked context.Context) error {
		return fn(locked, task)
	})
}

// Bound acquisition only. Waiting behind one KB must not occupy every worker
// or consume the model-call timeout before this task can claim any work.
// Once acquired, the original renewable lease and parent lifetime apply.
func withWorkerWikiGenerationLease(
	ctx context.Context, client *redis.Client, kb string, fn func(context.Context) error,
) error {
	leaseCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(100*time.Millisecond, func() { cancel(ErrWikiIngestConcurrent) })
	defer timer.Stop()
	err := wikilock.With(leaseCtx, client, kb, func(locked context.Context) error {
		if !timer.Stop() {
			return ErrWikiIngestConcurrent
		}
		return fn(locked)
	})
	if ctx.Err() == nil && errors.Is(context.Cause(leaseCtx), ErrWikiIngestConcurrent) {
		return ErrWikiIngestConcurrent
	}
	return err
}

// ProcessWikiIngest holds the same lease as finalize and historical reset.
func (s *wikiIngestService) ProcessWikiIngest(ctx context.Context, task *asynq.Task) error {
	return s.withGenerationLease(ctx, task, s.processWikiIngest)
}

// ProcessWikiFinalize cannot invalidate another batch's generation proof.
func (s *wikiIngestService) ProcessWikiFinalize(ctx context.Context, task *asynq.Task) error {
	return s.withGenerationLease(ctx, task, s.processWikiFinalize)
}
