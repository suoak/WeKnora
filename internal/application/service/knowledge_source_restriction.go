package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type knowledgeSourceRestrictionReader interface {
	KnowledgeSourceRestriction(context.Context, uint64, string, string) (types.KnowledgeSourceRestriction, error)
}

type sourceAdmissionTask struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
}

func (s *DataSourceService) KnowledgeBaseSourceRestriction(
	ctx context.Context,
	tenant uint64,
	kb string,
) (types.KnowledgeSourceRestriction, error) {
	if reader, ok := s.dsRepo.(knowledgeSourceRestrictionReader); ok {
		return reader.KnowledgeSourceRestriction(ctx, tenant, kb, "")
	}
	return types.UnknownKnowledgeSourceRestriction(), errors.New("source restriction metadata unavailable")
}

func checkKnowledgeSourceAdmission(
	ctx context.Context,
	repo interfaces.KnowledgeRepository,
	tenant uint64,
	kb, kid string,
) error {
	// Preserve the existing server-owned, independently fenced synchronous
	// publication/reconciliation path. This function never constructs it.
	if _, internal := types.SyncExecutionFromContext(ctx); internal {
		return nil
	}
	reader, ok := repo.(knowledgeSourceRestrictionReader)
	if !ok {
		return nil // partial mocks do not invent source policy tables
	}
	status, err := reader.KnowledgeSourceRestriction(ctx, tenant, kb, kid)
	if err != nil {
		return err
	}
	if status.Restricted {
		return &types.SourceProcessingRestrictedError{State: status.SourceState}
	}
	return nil
}

// WrapKnowledgeSourceAdmission is shared by Redis and Lite. Confirmed source
// restrictions stop before document/LLM work; transient DB errors, malformed
// payloads and unrelated task/permission errors retain existing semantics.
func WrapKnowledgeSourceAdmission(
	repo interfaces.KnowledgeRepository,
	next func(context.Context, *asynq.Task) error,
) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		if task == nil {
			return next(ctx, task)
		}
		switch task.Type() {
		case types.TypeDocumentProcess, types.TypeFeishuApplicationProcess, types.TypeManualProcess,
			types.TypeKnowledgePostProcess, types.TypeSummaryGeneration, types.TypeQuestionGeneration,
			types.TypeImageMultimodal, types.TypeKnowledgeAutoTag, types.TypeKnowledgeBaseProfile:
		default:
			return next(ctx, task)
		}
		var scope sourceAdmissionTask
		if json.Unmarshal(task.Payload(), &scope) != nil || scope.TenantID == 0 || scope.KnowledgeBaseID == "" ||
			(scope.KnowledgeID == "" && task.Type() != types.TypeKnowledgeBaseProfile) {
			return next(ctx, task)
		}
		stop := func(err error) error {
			var restricted *types.SourceProcessingRestrictedError
			if errors.As(err, &restricted) {
				logger.Warnf(
					ctx,
					"[SourceAdmission] task=%s knowledge_id=%s outcome=blocked source_state=%s",
					task.Type(),
					scope.KnowledgeID,
					restricted.State,
				)
				return errors.Join(asynq.SkipRetry, err)
			}
			return err
		}
		admit := func() error {
			return checkKnowledgeSourceAdmission(ctx, repo, scope.TenantID, scope.KnowledgeBaseID, scope.KnowledgeID)
		}
		if err := admit(); err != nil {
			return stop(err)
		}
		err := next(ctx, task)
		// A source can be revoked while the handler runs. Reclassify only a
		// positively confirmed current restriction, never a generic denial.
		if errors.Is(err, types.ErrLegacySourceRestricted) {
			if current := admit(); current != nil {
				var restricted *types.SourceProcessingRestrictedError
				if errors.As(current, &restricted) {
					return stop(current)
				}
			}
		}
		return err
	}
}
