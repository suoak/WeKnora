package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

var errProcessingExecution = errors.New("processing_execution_failed")

// Generation-bearing deliveries share this boundary in Asynq and Lite. Legacy
// payloads retain their old handler until the admission/migration batch is ready.
func (s *knowledgeService) ProcessDocument(ctx context.Context, task *asynq.Task) error {
	var payload types.DocumentProcessPayload
	if json.Unmarshal(task.Payload(), &payload) == nil && payload.Generation != 0 {
		return s.runProcessingExecution(ctx, payload.TenantID, payload.KnowledgeID, payload.Generation, time.Minute, func(ctx context.Context) error {
			return s.processDocument(ctx, task)
		})
	}
	return s.processDocument(ctx, task)
}

func (s *knowledgeService) ProcessManualUpdate(ctx context.Context, task *asynq.Task) error {
	var payload types.ManualProcessPayload
	if json.Unmarshal(task.Payload(), &payload) == nil && payload.Generation != 0 {
		return s.runProcessingExecution(ctx, payload.TenantID, payload.KnowledgeID, payload.Generation, time.Minute, func(ctx context.Context) error {
			return s.processManualUpdate(ctx, task)
		})
	}
	return s.processManualUpdate(ctx, task)
}

func (s *knowledgeService) runProcessingExecution(ctx context.Context, tenant uint64, knowledge string, generation int64, lease time.Duration, run func(context.Context) error) (result error) {
	provider, ok := s.repo.(interface {
		ProcessingFences() *repository.ProcessingFenceRepository
	})
	if !ok || lease < 30*time.Millisecond || run == nil {
		return errProcessingExecution
	}
	fences := provider.ProcessingFences()
	fence, err := fences.Claim(ctx, tenant, knowledge, generation, lease)
	if errors.Is(err, repository.ErrProcessingFence) {
		// Superseded, deleted, cancelled or already settled: no worker body runs.
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errProcessingExecution
	}
	work, cancel := context.WithCancelCause(ctx)
	work = types.WithProcessingExecution(work, types.ProcessingExecution{TenantID: tenant, KnowledgeID: knowledge, Generation: generation, ExecutionID: fence.ExecutionID})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				if fences.Renew(work, fence, lease) != nil {
					cancel(errProcessingExecution)
					return
				}
			}
		}
	}()
	returned := false
	defer func() {
		cancel(context.Canceled)
		<-stopped
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		settled := returned && (result == nil || errors.Is(result, asynq.SkipRetry))
		if fences.Release(cleanup, fence, settled) != nil && result == nil {
			result = errProcessingExecution
		}
	}()
	result = run(work)
	returned = true
	if cause := context.Cause(work); cause != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errProcessingExecution
	}
	return result
}
