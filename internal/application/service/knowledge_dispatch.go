package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

func feishuAdmissionID(tenant uint64, kb string, metadata map[string]string, hash, name string) string {
	// The logical identity excludes content revision: concurrent first fetches
	// of different revisions must collide on the Knowledge PK, not create K2.
	// Revision and attempt remain immutable fields of the accepted intent.
	identity, _ := json.Marshal([]string{fmt.Sprint(tenant), kb, metadata["datasource_id"], metadata["external_id"]})
	return uuid.NewSHA1(uuid.NameSpaceURL, identity).String()
}

func (s *knowledgeService) acceptFeishuKnowledgeDispatch(ctx context.Context, k *types.Knowledge, fileSvc interfaces.FileService, tags []string) (*types.Knowledge, error) {
	repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return k, ErrFeishuDispatchUnconfirmed
	}
	op, err := s.dispatchIntent(ctx, k)
	if err != nil {
		return k, err
	}
	if err = repo.AcceptKnowledgeDispatch(ctx, k, op); err != nil {
		// The commit may have succeeded. Retain the candidate blob unless a
		// bounded authoritative read proves that no row owns it.
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		current, readErr := s.repo.GetKnowledgeByID(readCtx, k.TenantID, k.ID)
		if readErr == nil && current != nil {
			if current.FileHash != k.FileHash || current.FileName != k.FileName {
				return current, ErrFeishuStagingRequired
			}
			if resumeErr := s.ResumeKnowledgeDispatch(readCtx, current); resumeErr != nil {
				return current, errors.Join(err, resumeErr)
			}
			return current, nil
		}
		var admission *types.KnowledgeDispatchAdmissionError
		definitelyNotCommitted := errors.As(err, &admission) && !admission.CommitUnknown
		if definitelyNotCommitted && (errors.Is(readErr, repository.ErrKnowledgeNotFound) || (readErr == nil && current == nil)) {
			_ = fileSvc.DeleteFile(readCtx, k.FilePath)
		}
		return k, err
	}
	if err = s.setAndAttachKnowledgeTags(ctx, k.TenantID, k.KnowledgeBaseID, k, tags); err != nil {
		return k, err
	}
	if err = s.publishKnowledgeDispatch(ctx, op); err != nil {
		return k, err
	}
	return k, nil
}

func (s *knowledgeService) dispatchIntent(ctx context.Context, k *types.Knowledge) (*types.TaskPendingOp, error) {
	// Resolve the same effective options used by normal file creation. The
	// snapshot is persisted once, not recomputed on every recovery delivery.
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, k.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.TenantID != k.TenantID {
		return nil, errors.New("dispatch KB owner mismatch")
	}
	overrides, err := k.ProcessOverrides()
	if err != nil {
		return nil, err
	}
	eff := ResolveProcessConfig(kb, overrides)
	attempt := s.tracker().LatestAttempt(ctx, k.ID)
	if attempt <= 0 {
		attempt = 1
	}
	task := types.DocumentProcessPayload{DispatchID: "document-" + k.ID, TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, FilePath: k.FilePath, FileName: k.FileName, FileType: k.FileType, EnableMultimodel: eff.EnableMultimodel, EnableQuestionGeneration: eff.QuestionGenerationConfig.Enabled, QuestionCount: eff.QuestionGenerationConfig.QuestionCount, Language: types.LanguageFromContextOrDefault(ctx), Attempt: attempt}
	var metadata map[string]string
	_ = json.Unmarshal(k.Metadata, &metadata)
	payload, err := json.Marshal(types.KnowledgeDispatch{PayloadVersion: 1, SourceID: metadata["datasource_id"], ExternalID: metadata["external_id"], AttemptID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(task.DispatchID+fmt.Sprint(attempt))).String(), Revision: k.FileHash, Task: task})
	if err != nil {
		return nil, err
	}
	return &types.TaskPendingOp{TenantID: k.TenantID, TaskType: types.TypeDocumentProcess, Scope: types.KnowledgeDispatchScope, ScopeID: k.ID, DedupKey: task.DispatchID, Op: "pending", Payload: payload, EnqueuedAt: time.Now()}, nil
}

// publishKnowledgeDispatch acknowledges actual queue delivery, never merely
// the presence of a DB record. Failed acknowledgement is safely replayable.
func (s *knowledgeService) publishKnowledgeDispatch(ctx context.Context, op *types.TaskPendingOp) error {
	repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return ErrFeishuDispatchUnconfirmed
	}
	var intent types.KnowledgeDispatch
	if err := json.Unmarshal(op.Payload, &intent); err != nil {
		return err
	}
	if (intent.PayloadVersion != 1 && intent.PayloadVersion != 2 && intent.PayloadVersion != 3 && intent.PayloadVersion != 4 && intent.PayloadVersion != 5) || intent.Task.KnowledgeID != op.ScopeID || intent.Task.TenantID != op.TenantID || intent.Task.DispatchID != op.DedupKey || intent.Task.Attempt <= 0 {
		return errors.New("invalid durable dispatch identity")
	}
	if intent.PayloadVersion >= 4 && op.TaskType != types.TypeFeishuApplicationProcess {
		return errors.New("application task type required")
	}
	if intent.PayloadVersion < 4 && op.TaskType == types.TypeFeishuApplicationProcess {
		return errors.New("application envelope required")
	}
	if intent.PayloadVersion == 3 || intent.PayloadVersion == 5 {
		p, ok := s.personalRecovery.(*PersonalRecovery)
		if !ok {
			return errors.New("first-create dispatcher unavailable")
		}
		ready, err := p.firstCreateDispatchable(ctx, intent)
		if err != nil {
			return err
		}
		if !ready {
			return ErrFeishuDispatchUnconfirmed
		}
	} else if intent.PayloadVersion == 2 || intent.PayloadVersion == 4 {
		if s.personalRecovery == nil {
			return errors.New("personal recovery dispatcher unavailable")
		}
		ready, err := s.personalRecovery.Dispatchable(ctx, intent)
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
	} else if intent.Task.PersonalAttemptID != "" {
		return errors.New("personal attempt requires dispatch v2")
	}
	if op.Op == "completed" {
		return nil
	}
	if op.Op == "processed" {
		current, err := s.repo.GetKnowledgeByID(ctx, op.TenantID, op.ScopeID)
		if err != nil {
			return err
		}
		if current.ParseStatus == types.ParseStatusCompleted || current.ParseStatus == types.ParseStatusFinalizing {
			return repo.SetKnowledgeDispatchState(ctx, op.ID, "completed")
		}
		if current.ParseStatus != types.ParseStatusProcessing {
			return repo.SetKnowledgeDispatchState(ctx, op.ID, "attention")
		}
		return s.publishDurablePostProcess(ctx, op, intent.Task)
	}
	if op.Op == "attention" || op.Op == "obsolete" {
		return ErrFeishuDispatchUnconfirmed
	}
	payload, err := json.Marshal(intent.Task)
	if err != nil {
		return err
	}
	task := asynq.NewTask(op.TaskType, payload, documentProcessTaskOptions(s.config, asynq.MaxRetry(3), asynq.TaskID(op.DedupKey))...)
	_, err = s.task.Enqueue(task)
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) && !errors.Is(err, asynq.ErrDuplicateTask) {
		_ = repo.RecordKnowledgeDispatchFailure(ctx, op.ID, "queue_unavailable_or_unknown")
		return fmt.Errorf("publish knowledge %s: %w", op.ScopeID, err)
	}
	if err != nil {
		if err = s.confirmDispatchConflict(ctx, op.TaskType, types.QueueDefault, op.DedupKey, op); err != nil {
			return err
		}
	}
	if err := repo.SetKnowledgeDispatchState(ctx, op.ID, "dispatched"); err != nil {
		_ = repo.RecordKnowledgeDispatchFailure(ctx, op.ID, "dispatch_confirmation_unknown")
		return err
	}
	return nil
}

func (s *knowledgeService) confirmDispatchConflict(ctx context.Context, taskType, queue, id string, op *types.TaskPendingOp) error {
	inspector, ok := s.taskInspector.(interfaces.RuntimeTaskInspector)
	if !ok {
		return ErrFeishuDispatchUnconfirmed
	}
	existing, supported, err := inspector.GetRuntimeTask(ctx, queue, id)
	if errors.Is(err, asynq.ErrQueueNotFound) && s.personalRecovery != nil && s.redisClient != nil {
		// A warm Asynq client caches SADD registration. After a Redis flush
		// it can enqueue a task while leaving the queue registry absent.
		// Rebuild registration through the public client API, for this exact
		// personal intent only. Never scan queues or assume delivery succeeded.
		var intent types.KnowledgeDispatch
		if decodeErr := json.Unmarshal(op.Payload, &intent); decodeErr == nil && (intent.PayloadVersion == 2 && taskType == types.TypeDocumentProcess || intent.PayloadVersion == 4 && taskType == types.TypeFeishuApplicationProcess) && intent.Task.PersonalAttemptID != "" {
			payload, marshalErr := json.Marshal(intent.Task)
			if marshalErr != nil {
				return marshalErr
			}
			fresh := asynq.NewClientFromRedisClient(s.redisClient)
			_, replayErr := fresh.Enqueue(asynq.NewTask(taskType, payload), documentProcessTaskOptions(s.config, asynq.TaskID(id))...)
			// This client borrows the existing Redis connection; do not close
			// the shared connection. There is no background goroutine to stop.
			if replayErr != nil && !errors.Is(replayErr, asynq.ErrTaskIDConflict) && !errors.Is(replayErr, asynq.ErrDuplicateTask) {
				return replayErr
			}
			existing, supported, err = inspector.GetRuntimeTask(ctx, queue, id)
		}
	}
	if err != nil {
		return err
	}
	if !supported || existing == nil || existing.KnowledgeID != op.ScopeID || existing.TenantID != op.TenantID || existing.Type != taskType {
		return ErrFeishuDispatchUnconfirmed
	}
	switch existing.State {
	case types.RuntimeTaskArchived:
		supported, err = inspector.RunRuntimeTask(ctx, queue, id)
		if err != nil {
			return err
		}
		if !supported {
			return ErrFeishuDispatchUnconfirmed
		}
	case types.RuntimeTaskActive, types.RuntimeTaskPending, types.RuntimeTaskScheduled, types.RuntimeTaskRetry:
	default:
		return ErrFeishuDispatchUnconfirmed
	}
	return nil
}

type knowledgeDispatchContextKey struct{}

func (s *knowledgeService) publishDurablePostProcess(ctx context.Context, op *types.TaskPendingOp, parent types.DocumentProcessPayload) error {
	payload, err := json.Marshal(types.KnowledgePostProcessPayload{TenantID: parent.TenantID, KnowledgeBaseID: parent.KnowledgeBaseID, KnowledgeID: parent.KnowledgeID, Attempt: parent.Attempt, Language: parent.Language})
	if err != nil {
		return err
	}
	id := fmt.Sprintf("document-post-%s-%d", parent.KnowledgeID, parent.Attempt)
	opts := append(knowledgePostProcessTaskOptions(), asynq.TaskID(id))
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeKnowledgePostProcess, payload, opts...))
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		return s.confirmDispatchConflict(ctx, types.TypeKnowledgePostProcess, types.QueuePostProcess, id, op)
	}
	if err != nil {
		if repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository); ok {
			_ = repo.RecordKnowledgeDispatchFailure(ctx, op.ID, "postprocess_dispatch_unknown")
		}
	}
	return err
}

func (s *knowledgeService) ResumeKnowledgeDispatch(ctx context.Context, k *types.Knowledge) error {
	repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return ErrFeishuDispatchUnconfirmed
	}
	op, err := repo.GetKnowledgeDispatch(ctx, k.TenantID, k.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Legacy rows are eligible only before processing; a failed parser is
		// different from a failed enqueue and must not silently reparse old data.
		if k.FilePath == "" || (k.ParseStatus != types.ParseStatusPending && !(k.ParseStatus == types.ParseStatusFailed && k.ErrorMessage == "Failed to enqueue processing task")) {
			return ErrFeishuDispatchUnconfirmed
		}
		op, err = s.dispatchIntent(ctx, k)
		if err != nil {
			return err
		}
		if err = repo.WithKnowledgeDispatchLock(ctx, k.ID, func(ctx context.Context) error {
			fresh, err := s.repo.GetKnowledgeByID(ctx, k.TenantID, k.ID)
			if err != nil {
				return err
			}
			if fresh.FilePath != k.FilePath || fresh.FileHash != k.FileHash || (fresh.ParseStatus != types.ParseStatusPending && !(fresh.ParseStatus == types.ParseStatusFailed && fresh.ErrorMessage == "Failed to enqueue processing task")) {
				return ErrFeishuDispatchUnconfirmed
			}
			return repo.EnsureKnowledgeDispatch(ctx, k, op)
		}); err != nil {
			return err
		}
		op, err = repo.GetKnowledgeDispatch(ctx, k.TenantID, k.ID)
	}
	if err != nil {
		return err
	}
	return s.publishKnowledgeDispatch(ctx, op)
}

func (s *knowledgeService) RecoverKnowledgeDispatches(ctx context.Context) error {
	var personalErr error
	if s.personalRecovery != nil {
		personalErr = s.personalRecovery.Reconcile(ctx)
	}
	repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return ErrFeishuDispatchUnconfirmed
	}
	var after int64
	var failures []error
	if personalErr != nil {
		failures = append(failures, personalErr)
	}
	scanned, recovered, pending, maxRetries := 0, 0, 0, 0
	var oldest time.Time
	defer func() {
		if scanned > 0 {
			wait := time.Duration(0)
			if !oldest.IsZero() {
				wait = time.Since(oldest)
			}
			logger.Infof(ctx, "Knowledge dispatch scan: scanned=%d pending=%d recovered=%d failed=%d max_retries=%d oldest_wait_seconds=%.0f", scanned, pending, recovered, len(failures), maxRetries, wait.Seconds())
		}
	}()
	for {
		ops, err := repo.ListKnowledgeDispatches(ctx, after, 100)
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return errors.Join(failures...)
		}
		for _, op := range ops {
			after = op.ID
			scanned++
			if op.Op == "pending" || op.Op == "recovery" {
				pending++
				if oldest.IsZero() || op.EnqueuedAt.Before(oldest) {
					oldest = op.EnqueuedAt
				}
			}
			if op.FailCount > maxRetries {
				maxRetries = op.FailCount
			}
			if err := s.publishKnowledgeDispatch(ctx, op); err != nil {
				failures = append(failures, err)
			} else {
				recovered++
			}
		}
	}
}

func (s *knowledgeService) processDurableDocument(ctx context.Context, task *asynq.Task, payload types.DocumentProcessPayload) error {
	if s.personalRecovery != nil {
		k, err := s.repo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
		if err != nil {
			return err
		}
		if k.Channel == "feishu_personal" {
			return errors.New("legacy personal task requires fenced staging recovery")
		}
	}

	repo, ok := s.repo.(interfaces.KnowledgeDispatchRepository)
	if !ok {
		return ErrFeishuDispatchUnconfirmed
	}
	return repo.WithKnowledgeDispatchLock(ctx, payload.KnowledgeID, func(ctx context.Context) error {
		op, err := repo.GetKnowledgeDispatch(ctx, payload.TenantID, payload.KnowledgeID)
		if err != nil {
			return err
		}
		var intent types.KnowledgeDispatch
		if err = json.Unmarshal(op.Payload, &intent); err != nil {
			return err
		}
		// Require exactly the accepted payload, including file path and attempt.
		a, _ := json.Marshal(payload)
		b, _ := json.Marshal(intent.Task)
		if string(a) != string(b) {
			return errors.New("document payload differs from durable intent")
		}
		if op.Op == "completed" || op.Op == "processed" || op.Op == "obsolete" {
			return nil
		}
		if op.Op == "attention" {
			return ErrFeishuDispatchUnconfirmed
		}
		k, err := s.repo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
		if errors.Is(err, repository.ErrKnowledgeNotFound) {
			return repo.SetKnowledgeDispatchState(ctx, op.ID, "obsolete")
		}
		if err != nil {
			return err
		}
		if k.FileHash != intent.Revision || k.FilePath != payload.FilePath || k.ParseStatus == types.ParseStatusCancelled || k.ParseStatus == types.ParseStatusDeleting || attemptSuperseded(ctx, s.tracker(), k.ID, payload.Attempt) {
			return repo.SetKnowledgeDispatchState(ctx, op.ID, "obsolete")
		}
		if k.ParseStatus == types.ParseStatusCompleted || k.ParseStatus == types.ParseStatusFinalizing {
			return repo.SetKnowledgeDispatchState(ctx, op.ID, "completed")
		}
		// Never use a dispatch replay as permission to wipe partially indexed data.
		chunks, err := s.chunkRepo.ListAllChunksByKnowledgeID(ctx, k.TenantID, k.ID)
		if err != nil {
			return err
		}
		if len(chunks) > 0 {
			if err = repo.SetKnowledgeDispatchState(ctx, op.ID, "attention"); err != nil {
				return err
			}
			return ErrFeishuDispatchUnconfirmed
		}
		if err = repo.SetKnowledgeDispatchState(ctx, op.ID, "processing"); err != nil {
			return err
		}
		ctx = context.WithValue(ctx, knowledgeDispatchContextKey{}, op)
		err = s.processDocument(ctx, task)
		if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		state := "recovery"
		latest, latestErr := repo.GetKnowledgeDispatch(ctx, k.TenantID, k.ID)
		if latestErr != nil {
			return errors.Join(err, latestErr)
		}
		if latest.Op == "processed" {
			state = "processed"
		}
		current, readErr := s.repo.GetKnowledgeByID(ctx, k.TenantID, k.ID)
		if readErr == nil && current != nil && (current.ParseStatus == types.ParseStatusCompleted || current.ParseStatus == types.ParseStatusFinalizing) {
			state = "completed"
		}
		if err == nil && readErr == nil && current != nil && current.ParseStatus == types.ParseStatusFailed {
			state = "attention"
			_ = repo.RecordKnowledgeDispatchFailure(ctx, op.ID, "document_processing_terminal")
		}
		saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return errors.Join(err, readErr, repo.SetKnowledgeDispatchState(saveCtx, op.ID, state))
	})
}
