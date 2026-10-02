package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

func (s *DataSourceService) executionRepo() (interfaces.SyncExecutionRepository, error) {
	r, ok := s.syncLogRepo.(interfaces.SyncExecutionRepository)
	if !ok {
		return nil, errors.New("sync repository does not support execution fencing")
	}
	return r, nil
}

// ProcessSync claims a DB-issued generation before touching a connector. Queue
// IDs and retry counters are never ownership. Duplicate deliveries do no work.
func (s *DataSourceService) ProcessSync(ctx context.Context, task *asynq.Task) (runErr error) {
	var payload types.DataSourceSyncPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("%w: invalid sync payload", asynq.SkipRetry)
	}
	repo, err := s.executionRepo()
	if err != nil {
		return err
	}
	e, claimed, err := repo.ClaimExecution(ctx, payload.DataSourceID, payload.SyncLogID, payload.TenantID)
	if err != nil {
		return err
	}
	if !claimed {
		logger.Infof(ctx, "sync ds=%s run=%s phase=claim category=duplicate_or_terminal",
			payload.DataSourceID, payload.SyncLogID)
		return nil
	}
	ctx = types.WithSyncExecution(ctx, e)
	workerCtx, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				beatCtx, beatCancel := context.WithTimeout(workerCtx, 10*time.Second)
				valid, beatErr := repo.HeartbeatExecution(beatCtx, e)
				beatCancel()
				if beatErr != nil || !valid {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		if recover() != nil {
			runErr = errors.New("sync worker panicked")
		}
		close(stop)
		cancel()
		<-done
		// Timeout/panic/storage failure must release only THIS generation using
		// a fresh bounded DB context, not the canceled network context.
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()
		log, readErr := s.syncLogRepo.FindByID(finishCtx, e.SyncLogID)
		if readErr != nil {
			if runErr == nil {
				runErr = readErr
			}
			return
		}
		if log.Status != types.SyncLogStatusRunning || !log.ExecutionClaimed ||
			log.ExecutionGeneration != e.Generation {
			if log.ExecutionGeneration != e.Generation || errors.Is(runErr, types.ErrStaleSyncExecution) {
				runErr = nil
			}
			return
		}
		if runErr == nil {
			runErr = errors.New("sync attempt ended without finalization")
		}
		ds, readErr := s.dsRepo.FindByID(finishCtx, e.DataSourceID)
		if readErr != nil {
			if runErr == nil {
				runErr = readErr
			}
			return
		}
		log.Status = types.SyncLogStatusFailed
		log.FinishedAt = timePtr(time.Now().UTC())
		log.ErrorMessage = "Sync execution interrupted"
		ds.ErrorMessage = log.ErrorMessage
		ds.Status = types.DataSourceStatusError
		if syncTaskCanRetry(ctx, runErr) {
			log.Status = types.SyncLogStatusRunning
			log.FinishedAt = nil
		}
		valid, writeErr := repo.WriteExecution(finishCtx, e, ds, log, true)
		if writeErr != nil {
			runErr = writeErr
			return
		}
		if !valid {
			runErr = nil
		}
	}()
	return s.processSyncAttempt(workerCtx, task)
}

func (s *DataSourceService) checkSyncExecution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Pure item-classification unit callers do not carry execution context;
	// production ProcessSync always supplies it before any connector call.
	e, ok := types.SyncExecutionFromContext(ctx)
	if !ok {
		return nil
	}
	repo, err := s.executionRepo()
	if err != nil {
		return err
	}
	valid, err := repo.HeartbeatExecution(ctx, e)
	if err != nil {
		return err
	}
	if !valid {
		return types.ErrStaleSyncExecution
	}
	return nil
}

func syncTaskCanRetry(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, asynq.SkipRetry) || errors.Is(err, types.ErrStaleSyncExecution) {
		return false
	}
	attempt, maxRetry, ok := types.TaskRetryMetadataFromContext(ctx)
	if !ok {
		attempt, ok = asynq.GetRetryCount(ctx)
		maxRetry, _ = asynq.GetMaxRetry(ctx)
	}
	return ok && attempt < maxRetry
}

func (s *DataSourceService) writeSyncExecution(
	ctx context.Context, ds *types.DataSource, log *types.SyncLog, release bool,
) error {
	e, ok := types.SyncExecutionFromContext(ctx)
	if !ok {
		return errors.New("missing sync execution ownership")
	}
	repo, err := s.executionRepo()
	if err != nil {
		return err
	}
	valid, err := repo.WriteExecution(ctx, e, ds, log, release)
	if err != nil {
		return err
	}
	if !valid {
		logger.Infof(ctx, "sync ds=%s run=%s phase=write category=stale_result_discarded", e.DataSourceID, e.SyncLogID)
		return types.ErrStaleSyncExecution
	}
	return nil
}

// Connector/application failures already consumed request-layer retries. They
// terminate the logical run, rather than replaying the sync on another queue retry.
func (s *DataSourceService) failSyncAttempt(
	ctx context.Context, ds *types.DataSource, log *types.SyncLog, cause error, message string,
) error {
	terminalErr := errors.Join(asynq.SkipRetry, cause)
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		// The batch connector may return a resumable cursor with its error.
		// Persist that snapshot through the same fence, even after network cancel.
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.writeSyncExecution(dbCtx, ds, log, false); err != nil {
			return err
		}
		return cause // infrastructure timeout: wrapper releases/retries the attempt
	}
	if err := s.updateSyncRunResult(ctx, ds, log, &types.SyncResult{}, nil,
		types.SyncLogStatusFailed, message, ds.Status == types.DataSourceStatusPaused); err != nil {
		return err
	}
	return terminalErr
}
