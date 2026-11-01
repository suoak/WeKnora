package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func decodeExactRetry(row *types.TaskPendingOp) (*types.PersonalExactRetry, error) {
	var intent types.PersonalExactRetry
	if row == nil || row.ID <= 0 || row.Scope != types.PersonalExactRetryScope ||
		row.TaskType != types.TypeDocumentProcess || json.Unmarshal(row.Payload, &intent) != nil ||
		intent.Version != 1 || intent.Source.TenantID != row.TenantID ||
		intent.Source.KnowledgeID == "" || intent.Source.AttemptID == "" ||
		intent.Source.ExpectedActive != "" || intent.Source.Generation+1 != intent.SealedGeneration ||
		row.ScopeID != exactRetryKey(intent.Source.KnowledgeID, intent.SealedGeneration) ||
		intent.Task.ExactRetryTicketID != row.ID || intent.Task.KnowledgeID != intent.Source.KnowledgeID ||
		intent.Task.PersonalAttemptID != intent.Source.AttemptID || intent.Task.TenantID != row.TenantID ||
		intent.Task.KnowledgeBaseID != intent.Source.KnowledgeBaseID ||
		intent.Task.CandidateVersionID != intent.Source.CandidateVersionID ||
		!intent.Task.FirstCreate || intent.SourceReceiptID <= 0 || intent.OriginalOperationID <= 0 ||
		len(intent.SourceReceiptSHA) != 64 || len(intent.StageSHA256) != 64 || len(intent.OriginalDispatchSHA) != 64 ||
		intent.NotAfter.IsZero() || exactReceiptSourceIdentity(intent.Source) != intent.Source {
		return nil, repository.ErrPersonalIdentity
	}
	return &intent, nil
}

func (p *PersonalRecovery) loadExactRetry(
	ctx context.Context, id int64,
) (*types.TaskPendingOp, *types.PersonalExactRetry, error) {
	var row types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("id=? AND scope=?", id,
		types.PersonalExactRetryScope).Take(&row).Error; err != nil {
		return nil, nil, err
	}
	intent, err := decodeExactRetry(&row)
	return &row, intent, err
}

func (p *PersonalRecovery) processExactRetryDocument(ctx context.Context, payload types.DocumentProcessPayload) error {
	row, intent, err := p.loadExactRetry(ctx, payload.ExactRetryTicketID)
	if err != nil {
		return errors.Join(err, asynq.SkipRetry)
	}
	wanted, _ := json.Marshal(intent.Task)
	supplied, _ := json.Marshal(payload)
	if string(wanted) != string(supplied) {
		return errors.Join(repository.ErrPersonalIdentity, asynq.SkipRetry)
	}
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	maySeal := false
	err = dispatch.WithKnowledgeDispatchLock(ctx, payload.KnowledgeID, func(ctx context.Context) error {
		row, intent, err = p.loadExactRetry(ctx, row.ID)
		if err != nil {
			return err
		}
		if row.Op == "completed" {
			return nil // Delivery acknowledgement only; never a content grant.
		}
		if row.Op != "pending" && row.Op != "dispatched" {
			return repository.ErrPersonalFence
		}
		maySeal = true
		sealed, until, enabled := exactRetryWindow(payload.KnowledgeID)
		if !enabled || sealed != intent.SealedGeneration || !intent.NotAfter.After(time.Now()) ||
			intent.NotAfter.After(until) {
			return repository.ErrPersonalFence
		}
		work, cancel := context.WithDeadline(ctx, intent.NotAfter)
		defer cancel()
		m, _, observed, err := p.exactRetrySubject(work, payload.KnowledgeID)
		if err != nil || observed == nil || observed.ID != intent.Source.AttemptID ||
			!applicationPayloadMatches(m, payload) {
			return repository.ErrPersonalIdentity
		}
		work, err = p.privateWorkerContext(work, m, observed)
		if err != nil {
			return err
		}
		check, err := p.reviewExactRetrySource(work, observed, sealed)
		if err != nil {
			return err
		}
		if check.identity != intent.Source {
			return repository.ErrPersonalFence
		}
		var original *types.TaskPendingOp
		owner, err := p.repo.ClaimReviewedExactRetry(work, observed, row, p.lease,
			func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
				return p.validateMutationAuthority(tx, current, check.validate)
			},
			func(tx *gorm.DB, owner *types.FeishuPersonalAttempt) error {
				return p.validateMutationAuthority(tx, owner, func(
					tx *gorm.DB, current *types.FeishuPersonalAttempt,
				) error {
					if !intent.NotAfter.After(time.Now()) {
						return repository.ErrPersonalFence
					}
					var oldTask types.DocumentProcessPayload
					original, oldTask, err = originalExactRetryOperation(tx, intent.Source)
					if err != nil || original == nil || original.ID != intent.OriginalOperationID ||
						exactRetrySHA(original.Payload) != intent.OriginalDispatchSHA {
						return repository.ErrPersonalFence
					}
					oldTask.ExactRetryTicketID = row.ID
					canonical, _ := json.Marshal(oldTask)
					if string(canonical) != string(wanted) {
						return repository.ErrPersonalIdentity
					}
					fresh := *check.checkedSource
					fresh.generation, fresh.nonce = current.Fence, current.OwnerNonce
					fresh.leaseUntil = *current.LeaseUntil
					if err := p.quarantineExactRetryStage(tx, &fresh, *intent, row.ID, observed); err != nil {
						return err
					}
					changed := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='attention'", original.ID).
						Update("op", "processing")
					if changed.Error != nil || changed.RowsAffected != 1 {
						return repository.ErrPersonalFence
					}
					original.Op = "processing"
					return nil
				})
			})
		if err != nil {
			return err
		}
		logger.Infof(work, "Personal exact retry claimed: knowledge_id=%s ticket_id=%d generation=%d",
			payload.KnowledgeID, row.ID, owner.Fence)
		if err := p.runExactClaimedFirstCreate(work, payload, m, owner, original); err != nil {
			return err
		}
		return p.finishExactRetryTicket(context.WithoutCancel(ctx), row.ID, "completed", "")
	})
	if err != nil {
		// A refused delivery is terminal. Never call RunRuntimeTask on an old
		// archived job or reset the source/original execution to pending.
		if maySeal {
			_ = p.finishExactRetryTicket(context.WithoutCancel(ctx), row.ID, "attention", exactRetryFailureClass(err))
		}
		logger.Warnf(ctx, "Personal exact retry stopped: knowledge_id=%s ticket_id=%d class=exact_retry_refused",
			payload.KnowledgeID, row.ID)
		return errors.Join(err, asynq.SkipRetry)
	}
	return nil
}

func (p *PersonalRecovery) runExactClaimedFirstCreate(
	ctx context.Context, payload types.DocumentProcessPayload, imported *types.FeishuPersonalImport,
	owner *types.FeishuPersonalAttempt, op *types.TaskPendingOp,
) error {
	work, cancel := context.WithCancel(ctx)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(p.lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-work.Done():
				return
			case <-ticker.C:
				if _, err := p.authorize(work, imported, owner); err != nil {
					cancel()
					return
				}
				if err := p.mutate(work, owner, p.lease, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); cancel(); <-stopped }()
	work = context.WithValue(work, knowledgeDispatchContextKey{}, op)
	work = context.WithValue(work, personalFirstCreateContextKey{}, true)
	return p.processExactFirstCreateConsumer(work, payload, owner, op.ID)
}

func (p *PersonalRecovery) finishExactRetryTicket(ctx context.Context, id int64, state, reason string) error {
	if state != "completed" && state != "attention" {
		return repository.ErrPersonalIdentity
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	row, intent, err := p.loadExactRetry(ctx, id)
	if err != nil {
		return err
	}
	return p.service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var resource types.FeishuPersonalResource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("import_id=?", intent.Source.ImportID).
			Take(&resource).Error; err != nil {
			return err
		}
		var attempt types.FeishuPersonalAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", intent.Source.AttemptID).
			Take(&attempt).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(row).Error; err != nil {
			return err
		}
		if row.Op == "completed" || row.Op == "attention" {
			return nil
		}
		if resource.CurrentAttemptID != attempt.ID || resource.LogicalKnowledgeID != intent.Source.KnowledgeID ||
			attempt.OwnerNonce != "" || attempt.LeaseUntil != nil {
			return repository.ErrPersonalFence // Never terminate another live delivery.
		}
		if state == "completed" {
			if attempt.Status != "indexed" || resource.Generation != intent.SealedGeneration+1 ||
				attempt.Fence != resource.Generation || resource.ActiveVersionID != intent.Source.CandidateVersionID {
				return repository.ErrPersonalFence
			}
			var receipt types.TaskPendingOp
			if err := tx.Where("scope=? AND scope_id=? AND tenant_id=? AND op='published'",
				types.PersonalExactReceiptScope, intent.Source.KnowledgeID, row.TenantID,
			).Take(&receipt).Error; err != nil {
				return err
			}
			var published types.PersonalExactReceipt
			expected := intent.Source
			expected.Generation = intent.SealedGeneration + 1
			if json.Unmarshal(receipt.Payload, &published) != nil || exactReceiptSourceIdentity(published) != expected {
				return repository.ErrPersonalFence
			}
		} else {
			beforeClaim := resource.Generation == intent.SealedGeneration && attempt.Fence+1 == resource.Generation
			afterAbort := resource.Generation == intent.SealedGeneration+2 && attempt.Fence+1 == resource.Generation
			if attempt.Status != "needs_attention" || resource.ActiveVersionID != "" || (!beforeClaim && !afterAbort) {
				return repository.ErrPersonalFence
			}
		}
		increment := 0
		if reason != "" {
			increment = 1
		}
		if err := tx.Model(row).Updates(map[string]any{
			"op":         state,
			"fail_count": gorm.Expr("fail_count+?", increment),
		}).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"ticket_id": id, "knowledge_id": intent.Source.KnowledgeID,
			"state": state, "failure_class": reason, "sealed_generation": intent.SealedGeneration,
			"observed_generation": resource.Generation,
		})
		if err != nil {
			return err
		}
		return tx.Create(&types.TaskPendingOp{
			TenantID: row.TenantID, Scope: types.PersonalExactRetryAuditScope,
			ScopeID: intent.Source.KnowledgeID, TaskType: "knowledge:exact_retry_audit", Op: state,
			DedupKey: row.DedupKey, Payload: payload, EnqueuedAt: time.Now().UTC(),
		}).Error
	})
}

func exactRetryFailureClass(err error) string {
	if errors.Is(err, core.ErrPermission) || errors.Is(err, core.ErrAuth) || errors.Is(err, ErrFeishuReconnect) {
		return "source_authorization_required"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "request_timeout"
	}
	var api *core.APIError
	if errors.As(err, &api) {
		switch core.TransportErrorCategory(api.Cause) {
		case "security_rejected":
			return "transport_security_rejected"
		case "timeout":
			return "request_timeout"
		case "truncated", "eof", "connection_reset", "connection_refused", "broken_pipe", "dns",
			"http2_goaway", "http2_stream", "http2_connection_lost":
			return "transport_unavailable"
		}
		if api.Category == core.ErrorCategoryRateLimit {
			return "provider_rate_limited"
		}
		if api.Category == core.ErrorCategoryTransient {
			return "provider_unavailable"
		}
	}
	return "source_or_execution_state_changed"
}
