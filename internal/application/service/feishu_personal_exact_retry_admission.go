package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This expiring server deployment policy is an execution interlock, never
// source authorization or a task/header parameter. No default global retry.
func exactRetryWindow(id string) (int64, time.Time, bool) {
	if os.Getenv("FEISHU_PERSONAL_EXACT_RETRY") != "1" || !exactPersonalRecoveryAllows(id) {
		return 0, time.Time{}, false
	}
	var generations map[string]int64
	if json.Unmarshal([]byte(os.Getenv("FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS")), &generations) != nil ||
		len(generations) == 0 || len(generations) > 8 || generations[id] <= 0 {
		return 0, time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, os.Getenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL"))
	if err != nil || !until.After(time.Now()) || until.Sub(time.Now()) > time.Hour {
		return 0, time.Time{}, false
	}
	return generations[id], until, true
}

func exactRetryKey(id string, sealed int64) string { return fmt.Sprintf("%s:%d", id, sealed) }

func (p *PersonalRecovery) exactRetrySubject(
	ctx context.Context, id string,
) (*types.FeishuPersonalImport, *types.FeishuPersonalResource, *types.FeishuPersonalAttempt, error) {
	var reference struct{ CurrentAttemptID string }
	err := p.service.db.WithContext(ctx).Table("feishu_personal_resources").Select("current_attempt_id").
		Where("logical_knowledge_id=?", id).Take(&reference).Error
	if err != nil {
		return nil, nil, nil, err
	}
	return p.repo.Load(ctx, reference.CurrentAttemptID)
}

func (p *PersonalRecovery) reviewExactRetrySource(
	ctx context.Context, owner *types.FeishuPersonalAttempt, sealed int64,
) (*personalExactPreflight, error) {
	check := &personalExactPreflight{observed: owner, sealedGeneration: sealed}
	if _, err := p.checkExactPersonalSource(ctx, owner, false, check); err != nil {
		return nil, err
	}
	if check.validate == nil || check.checkedSource == nil || check.identity.KnowledgeID == "" {
		return nil, errPersonalExactDocument
	}
	return check, nil
}

func originalExactRetryOperation(
	tx *gorm.DB, source types.PersonalExactReceipt,
) (*types.TaskPendingOp, types.DocumentProcessPayload, error) {
	var rows []types.TaskPendingOp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"scope=? AND scope_id=? AND tenant_id=?", types.KnowledgeDispatchScope, source.KnowledgeID, source.TenantID,
	).Limit(2).Find(&rows).Error; err != nil {
		return nil, types.DocumentProcessPayload{}, err
	}
	var dispatch types.KnowledgeDispatch
	if len(rows) != 1 || rows[0].Op != "attention" || rows[0].TaskType != types.TypeDocumentProcess ||
		json.Unmarshal(rows[0].Payload, &dispatch) != nil || dispatch.PayloadVersion != 3 {
		return nil, types.DocumentProcessPayload{}, repository.ErrPersonalFence
	}
	task := dispatch.Task
	if !task.FirstCreate || task.PersonalAttemptID != source.AttemptID ||
		task.KnowledgeID != source.KnowledgeID || task.KnowledgeBaseID != source.KnowledgeBaseID ||
		task.TenantID != source.TenantID || task.CandidateVersionID != source.CandidateVersionID ||
		task.DispatchID != "document-"+source.KnowledgeID || rows[0].DedupKey != task.DispatchID ||
		task.ExactRetryTicketID != 0 || task.Attempt <= 0 || dispatch.AttemptID != source.AttemptID ||
		task.FilePath != types.BuildResourcePath(source.ResourceHandle) ||
		task.FileURL != "" || task.URL != "" || len(task.Passages) != 0 ||
		strings.TrimPrefix(strings.ToLower(task.FileType), ".") != "docx" || task.EnableQuestionGeneration {
		return nil, types.DocumentProcessPayload{}, repository.ErrPersonalIdentity
	}
	return &rows[0], task, nil
}

// Admission never changes Knowledge, ownership, the original outbox or staged
// output. A separate intent is accepted only after complete live source proof.
func (p *PersonalRecovery) requestExactRetry(
	ctx context.Context, knowledge string, explicit bool,
) (*types.TaskPendingOp, error) {
	sealed, until, enabled := exactRetryWindow(knowledge)
	if !enabled {
		return nil, apperrors.NewConflictError("该文档的安全重试尚未启用；原文件和已有内容保持不变").
			WithDetails(map[string]any{"reason": "personal_exact_retry_not_enabled"})
	}
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	var accepted *types.TaskPendingOp
	err := dispatch.WithKnowledgeDispatchLock(ctx, knowledge, func(ctx context.Context) error {
		m, resource, owner, err := p.exactRetrySubject(ctx, knowledge)
		if err != nil {
			return err
		}
		if m.KnowledgeID != knowledge || resource.CurrentAttemptID != owner.ID ||
			resource.Generation != sealed || owner.Fence+1 != sealed || owner.Status != "needs_attention" ||
			owner.OwnerNonce != "" || owner.LeaseUntil != nil || resource.ActiveVersionID != "" {
			return repository.ErrPersonalFence
		}
		work, err := p.privateWorkerContext(ctx, m, owner)
		if err != nil {
			return err
		}
		check, err := p.reviewExactRetrySource(work, owner, sealed)
		if err != nil {
			return err
		}
		return p.repo.ReviewExactRetry(work, owner, sealed, func(
			tx *gorm.DB, current *types.FeishuPersonalAttempt,
		) error {
			return p.validateMutationAuthority(tx, current, func(
				tx *gorm.DB, current *types.FeishuPersonalAttempt,
			) error {
				if err := check.validate(tx, current); err != nil {
					return err
				}
				var previous []types.TaskPendingOp
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
					"scope=? AND scope_id=? AND tenant_id=?", types.PersonalExactRetryScope,
					exactRetryKey(knowledge, sealed), owner.CallerTenantID,
				).Order("id DESC").Limit(4).Find(&previous).Error; err != nil {
					return err
				}
				for _, existing := range previous {
					if existing.Op == "pending" || existing.Op == "dispatched" || existing.Op == "claimed" {
						accepted = &existing
						return nil
					}
				}
				if len(previous) > 0 && (!explicit || len(previous) >= 3) {
					return apperrors.NewConflictError("该执行已有终态记录，不会自动重放；请核查上次重试结果")
				}
				stage, err := inspectExactRetryStage(tx, check.identity, true)
				if err != nil {
					return err
				}
				receipt, err := loadExactRetryStagingReceipt(tx, check.identity, true)
				if err != nil {
					return err
				}
				op, task, err := originalExactRetryOperation(tx, check.identity)
				if err != nil {
					return err
				}
				receiptBytes, err := json.Marshal(receipt)
				if err != nil {
					return err
				}
				intent := types.PersonalExactRetry{
					Version: 1, SealedGeneration: sealed,
					Source: check.identity, StageSHA256: stage.SHA256, SourceReceiptID: receipt.ID,
					SourceReceiptSHA: exactRetrySHA(receiptBytes), NotAfter: until,
					OriginalOperationID: op.ID, OriginalDispatchSHA: exactRetrySHA(op.Payload), Task: task,
				}
				row := types.TaskPendingOp{
					TenantID: owner.CallerTenantID, Scope: types.PersonalExactRetryScope,
					ScopeID: exactRetryKey(knowledge, sealed), Op: "pending", TaskType: types.TypeDocumentProcess,
					DedupKey: "exact-retry-" + uuid.NewString(), Payload: json.RawMessage(`{}`),
					EnqueuedAt: time.Now().UTC(),
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				intent.Task.ExactRetryTicketID = row.ID
				payload, err := json.Marshal(intent)
				if err != nil {
					return err
				}
				if err := tx.Model(&row).Update("payload", payload).Error; err != nil {
					return err
				}
				row.Payload, accepted = payload, &row
				return nil
			})
		})
	})
	if err != nil {
		return nil, err
	}
	logger.Infof(ctx, "Personal exact retry admitted: knowledge_id=%s ticket_id=%d sealed_generation=%d",
		knowledge, accepted.ID, sealed)
	return accepted, nil
}

func exactRetryPublicError(err error) error {
	var app *apperrors.AppError
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, ErrFeishuReconnect) {
		return apperrors.NewForbiddenError("需要原导入用户重新完成飞书授权；文件仍保持隔离").
			WithDetails(map[string]any{"reason": "personal_exact_source_owner_reauthorization_required"})
	}
	if errors.Is(err, core.ErrPermission) || errors.Is(err, core.ErrAuth) || errors.Is(err, core.ErrAppScope) ||
		errors.Is(err, core.ErrUserScope) || errors.Is(err, access.ErrForbidden) ||
		errors.Is(err, access.ErrUnauthorized) {
		return apperrors.NewForbiddenError("当前飞书授权或知识库权限不足；原文件仍保持隔离").
			WithDetails(map[string]any{"reason": "personal_exact_retry_permission_denied"})
	}
	if errors.Is(err, repository.ErrPersonalFence) || errors.Is(err, repository.ErrPersonalIdentity) ||
		errors.Is(err, errPersonalExactDocument) || errors.Is(err, errExactWorkbookUnsupported) {
		return apperrors.NewConflictError("文档来源或文件版本未通过安全检查，请联系管理员核验；原文件保持不变").
			WithDetails(map[string]any{"reason": "personal_exact_retry_preflight_refused"})
	}
	return apperrors.NewServiceUnavailableError("暂时无法验证飞书文档，请稍后重试；原文件保持不变").
		WithDetails(map[string]any{"reason": "personal_exact_retry_preflight_unavailable"})
}
