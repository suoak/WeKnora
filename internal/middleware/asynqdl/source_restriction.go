package asynqdl

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

// RecordSourceRestriction archives an admission stop, not an exhausted body
// execution. Redis and Lite use the same minimal record and never read or
// change Knowledge to do so. Generic SkipRetry is intentionally excluded.
func RecordSourceRestriction(repo interfaces.TaskDeadLetterRepository, ctx context.Context, task *asynq.Task, err error) bool {
	var restricted *types.SourceProcessingRestrictedError
	if task == nil || !errors.Is(err, asynq.SkipRetry) || !errors.As(err, &restricted) ||
		(restricted.State != "hold" && restricted.State != "revoked") {
		return false
	}
	var scope struct {
		TenantID        uint64 `json:"tenant_id"`
		KnowledgeBaseID string `json:"knowledge_base_id"`
		KnowledgeID     string `json:"knowledge_id"`
		Attempt         int    `json:"attempt,omitempty"`
	}
	if json.Unmarshal(task.Payload(), &scope) != nil || scope.TenantID == 0 || scope.KnowledgeBaseID == "" ||
		(scope.KnowledgeID == "" && task.Type() != types.TypeKnowledgeBaseProfile) {
		return false
	}
	if repo == nil {
		return true
	}
	payload, _ := json.Marshal(scope)
	attempts := 1
	if retried, ok := asynq.GetRetryCount(ctx); ok {
		attempts = retried + 1
	} else if retried, _, ok := types.TaskRetryMetadataFromContext(ctx); ok {
		attempts = retried + 1
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	row := &types.TaskDeadLetter{
		TenantID: scope.TenantID, TaskType: task.Type(), Scope: types.TaskScopeKnowledgeBase,
		ScopeID: scope.KnowledgeBaseID, RelatedID: scope.KnowledgeID,
		Payload: payload, LastError: "source_processing_restricted:" + restricted.State,
		FailCount: attempts, FailedAt: time.Now(),
	}
	if repo.Insert(cleanup, row) != nil {
		logger.Warnf(ctx, "source admission audit persistence failed task=%s knowledge_id=%s", task.Type(), scope.KnowledgeID)
	}
	return true
}
