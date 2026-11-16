package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

type ordinaryEmbeddingTaskKey struct{}

func (s *knowledgeService) ordinaryEmbeddingStore() *repository.OrdinaryEmbeddingStore {
	if s.task == nil {
		return nil
	}
	if provider, ok := s.repo.(interface {
		OrdinaryEmbeddingStore() *repository.OrdinaryEmbeddingStore
	}); ok {
		return provider.OrdinaryEmbeddingStore()
	}
	return nil
}

func (s *knowledgeService) deferOrdinaryEmbedding(ctx context.Context, kb *types.KnowledgeBase, k *types.Knowledge, chunks []*types.Chunk, model embedding.Embedder, engine *retriever.CompositeRetrieveEngine, options ProcessChunksOptions, bytes int64) (bool, error) {
	store := s.ordinaryEmbeddingStore()
	parent, admitted := ctx.Value(ordinaryEmbeddingTaskKey{}).(types.DocumentProcessPayload)
	if !admitted || parent.Attempt < 1 || parent.OrdinaryEmbeddingID != 0 || store == nil || k.Channel != "web" || k.Type != "file" || kb.HasVectorStore() || len(chunks) < 1000 || len(chunks) > 100000 || options.EnableMultimodel || len(options.StoredImages) > 0 || !engine.SupportsOrdinaryEmbedding() || attemptFromCtx(ctx) < 1 {
		return false, nil
	}
	if _, _, _, exact := types.ExactDocumentIdentity(ctx); exact {
		return false, nil
	}
	if _, guarded := types.FeishuSourceGuardFromContext(ctx); guarded {
		return false, nil
	}
	plan := types.OrdinaryEmbeddingPlan{Task: types.DocumentProcessPayload{TenantID: k.TenantID, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID, FilePath: k.FilePath, FileName: k.FileName, FileType: k.FileType, Attempt: attemptFromCtx(ctx), Language: types.LanguageFromContextOrDefault(ctx)}, StorageBytes: bytes, Dimension: model.GetDimensions()}
	plan.KBStamp = repository.OrdinaryEmbeddingKBStamp(kb)
	for _, chunk := range chunks {
		plan.Inputs = append(plan.Inputs, types.OrdinaryEmbeddingInput{ChunkID: chunk.ID, SHA256: repository.OrdinaryEmbeddingChunkStamp(chunk)})
	}
	op, err := store.Start(ctx, k, plan)
	if err != nil {
		return true, err
	}
	if err = s.publishOrdinaryEmbedding(ctx, op); err != nil {
		// The committed outbox owns delivery now. Re-running the parser would
		// delete accepted chunks; the recovery loop retries only this intent.
		logger.Warnf(ctx, "Ordinary embedding accepted; queue publication deferred: knowledge=%s", k.ID)
	}
	return true, nil
}

func (s *knowledgeService) publishOrdinaryEmbedding(ctx context.Context, op *types.TaskPendingOp) error {
	plan, err := repository.DecodeOrdinaryEmbedding(op)
	if err != nil {
		return err
	}
	if op.Op == "completed" || op.Op == "attention" || op.Op == "cancelled" {
		return nil
	}
	if plan.LeaseUntil.After(time.Now()) {
		return nil
	}
	payload := plan.Task
	payload.OrdinaryEmbeddingID = op.ID
	payload.EmbeddingCursor = plan.Cursor
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeDocumentProcess, data), asynq.Queue(types.QueueDefault), asynq.TaskID(fmt.Sprintf("ordinary-embed:%d:%d", op.ID, plan.Cursor)), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}

func (s *knowledgeService) resumeOrdinaryEmbedding(ctx context.Context, payload types.DocumentProcessPayload) (bool, error) {
	store := s.ordinaryEmbeddingStore()
	if store == nil || payload.Attempt < 1 {
		return false, nil
	}
	op, err := store.Find(ctx, payload.TenantID, payload.KnowledgeID, payload.Attempt)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.publishOrdinaryEmbedding(ctx, op)
}

func (s *knowledgeService) processOrdinaryEmbedding(ctx context.Context, payload types.DocumentProcessPayload) error {
	store := s.ordinaryEmbeddingStore()
	if store == nil {
		return errors.New("ordinary embedding store unavailable")
	}
	if payload.PersonalAttemptID != "" || payload.DispatchID != "" || payload.FeishuAuthMode != "" || payload.FirstCreate || payload.OrdinaryEmbeddingID < 1 {
		return asynq.SkipRetry
	}
	op, err := store.Find(ctx, payload.TenantID, payload.KnowledgeID, payload.Attempt)
	if err != nil {
		return err
	}
	if op.ID != payload.OrdinaryEmbeddingID {
		return asynq.SkipRetry
	}
	plan, err := repository.DecodeOrdinaryEmbedding(op)
	if err != nil {
		return errors.Join(asynq.SkipRetry, err)
	}
	if plan.Task.KnowledgeBaseID != payload.KnowledgeBaseID || plan.Cursor != payload.EmbeddingCursor || op.Op == "completed" || op.Op == "attention" || op.Op == "cancelled" {
		return nil
	}
	ctx = types.WithExecutionTenant(ctx, payload.TenantID)
	ctx = types.WithBackgroundTask(ctx)
	ctx = withAttempt(ctx, payload.Attempt)
	ctx = context.WithValue(ctx, types.LanguageContextKey, plan.Task.Language)
	tenant, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	kb, err := s.processingKnowledgeBase(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return err
	}
	ctx, err = access.WithKBTaskWrite(ctx, kb, payload.TenantID)
	if err != nil {
		return errors.Join(asynq.SkipRetry, err)
	}
	if op.Op == "indexed" || op.Op == "finalizing" {
		return s.finishOrdinaryEmbedding(ctx, store, op, plan)
	}
	op, plan, chunks, err := store.Claim(ctx, op.ID, payload.TenantID, payload.EmbeddingCursor)
	if errors.Is(err, repository.ErrOrdinaryEmbeddingBusy) {
		return nil
	}
	if errors.Is(err, repository.ErrOrdinaryEmbeddingChanged) || errors.Is(err, gorm.ErrRecordNotFound) {
		return store.Reject(ctx, payload.OrdinaryEmbeddingID, payload.TenantID, payload.EmbeddingCursor)
	}
	if err != nil {
		return err
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-tick.C:
				if err := store.Renew(leaseCtx, op.ID, payload.TenantID, plan.Cursor, plan.LeaseToken); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	writeCtx := types.WithOrdinaryEmbeddingWrite(leaseCtx, func(ctx context.Context, db *gorm.DB, write func(*gorm.DB) error) error {
		return store.WithWrite(ctx, db, op.ID, payload.TenantID, plan.Cursor, plan.LeaseToken, write)
	})
	k, err := s.repo.GetKnowledgeByID(writeCtx, payload.TenantID, payload.KnowledgeID)
	if err == nil {
		var model embedding.Embedder
		model, err = s.modelService.GetEmbeddingModel(writeCtx, kb.EmbeddingModelID)
		if err == nil && model.GetDimensions() != plan.Dimension {
			err = repository.ErrOrdinaryEmbeddingChanged
		}
		if err == nil {
			var engine *retriever.CompositeRetrieveEngine
			engine, err = retriever.CreateRetrieveEngineForKB(writeCtx, s.retrieveEngine, s.ownership, payload.TenantID, kb.VectorStoreID)
			if err == nil && !engine.SupportsOrdinaryEmbedding() {
				err = repository.ErrOrdinaryEmbeddingChanged
			}
			if err == nil && len(chunks) > 0 {
				infos := make([]*types.IndexInfo, 0, len(chunks))
				for _, chunk := range chunks {
					infos = append(infos, &types.IndexInfo{Content: buildKnowledgeIndexContent(k, chunk.EmbeddingContent()), SourceID: chunk.ID, ChunkID: chunk.ID, SourceType: types.ChunkSourceType, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID, IsEnabled: true})
				}
				err = engine.BatchIndex(writeCtx, model, infos)
			}
		}
	}
	if err == nil {
		op, err = store.Advance(writeCtx, op.ID, payload.TenantID, plan.Cursor, plan.LeaseToken)
	}
	if err != nil {
		permanent := errors.Is(err, repository.ErrOrdinaryEmbeddingChanged) || errors.Is(err, gorm.ErrRecordNotFound)
		// Cleanup only this still-owned lease, using a bounded independent
		// context after a native deadline. Never delete chunks or vectors.
		releaseCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		releaseErr := store.Release(releaseCtx, payload.OrdinaryEmbeddingID, payload.TenantID, plan.Cursor, plan.LeaseToken, permanent)
		stop()
		if permanent {
			return errors.Join(asynq.SkipRetry, releaseErr)
		}
		return errors.Join(err, releaseErr)
	}
	logger.Infof(ctx, "Ordinary embedding persisted: knowledge=%s completed=%d total=%d", payload.KnowledgeID, min(plan.Cursor+repository.OrdinaryEmbeddingWindow, len(plan.Inputs)), len(plan.Inputs))
	if op.Op == "indexed" {
		plan, _ = repository.DecodeOrdinaryEmbedding(op)
		return s.finishOrdinaryEmbedding(ctx, store, op, plan)
	}
	// A queue error cannot turn committed progress into re-parsing. The
	// deterministic next window remains in the outbox for recovery.
	if err := s.publishOrdinaryEmbedding(ctx, op); err != nil {
		logger.Warnf(ctx, "Ordinary embedding next window queued for recovery: knowledge=%s", payload.KnowledgeID)
	}
	return nil
}

func (s *knowledgeService) finishOrdinaryEmbedding(ctx context.Context, store *repository.OrdinaryEmbeddingStore, op *types.TaskPendingOp, plan types.OrdinaryEmbeddingPlan) error {
	k, err := store.Finish(ctx, op.ID, op.TenantID)
	if errors.Is(err, repository.ErrOrdinaryEmbeddingChanged) || errors.Is(err, gorm.ErrRecordNotFound) {
		return store.Reject(ctx, op.ID, op.TenantID, plan.Cursor)
	}
	if err != nil {
		return err
	}
	if k.ParseStatus == types.ParseStatusFinalizing || k.ParseStatus == types.ParseStatusCompleted {
		return store.DispatchedPostprocess(ctx, op.ID, op.TenantID)
	}
	s.endStage(ctx, k.ID, types.StageEmbedding, types.JSONMap{"vectors_written": len(plan.Inputs), "storage_bytes": plan.StorageBytes})
	s.skipStage(ctx, k.ID, types.StageMultimodal, "skipped")
	post := types.KnowledgePostProcessPayload{TenantID: k.TenantID, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID, Attempt: plan.Task.Attempt, Language: plan.Task.Language}
	data, err := json.Marshal(post)
	if err != nil {
		return err
	}
	opts := knowledgePostProcessTaskOptions()
	opts = append(opts, asynq.TaskID(fmt.Sprintf("ordinary-post:%d", op.ID)))
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeKnowledgePostProcess, data), opts...)
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) && !errors.Is(err, asynq.ErrDuplicateTask) {
		return err
	}
	return store.DispatchedPostprocess(ctx, op.ID, op.TenantID)
}

func (s *knowledgeService) recoverOrdinaryEmbeddings(ctx context.Context) error {
	store := s.ordinaryEmbeddingStore()
	if store == nil {
		return nil
	}
	var after int64
	var failures []error
	for scanned := 0; scanned < 1000; {
		ops, err := store.List(ctx, after, 100)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if len(ops) == 0 {
			return errors.Join(failures...)
		}
		for _, op := range ops {
			if ctx.Err() != nil {
				return errors.Join(append(failures, ctx.Err())...)
			}
			after = op.ID
			scanned++
			plan, err := repository.DecodeOrdinaryEmbedding(op)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if !plan.LeaseUntil.After(time.Now()) && plan.BatchAttempts >= 4 {
				if err := store.Reject(ctx, op.ID, op.TenantID, plan.Cursor); err != nil {
					failures = append(failures, err)
				}
				continue
			}
			if err := s.publishOrdinaryEmbedding(ctx, op); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
