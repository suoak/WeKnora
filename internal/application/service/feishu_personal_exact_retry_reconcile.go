package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type exactRetryAdmissionBudget struct {
	count    int
	busy     bool
	terminal bool
	next     time.Time
}

type exactRetryController struct {
	mu      sync.Mutex
	budgets map[string]exactRetryAdmissionBudget
}

func newExactRetryController() *exactRetryController {
	return &exactRetryController{budgets: make(map[string]exactRetryAdmissionBudget)}
}

func (c *exactRetryController) begin(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.budgets[key]
	if b.busy || b.terminal || b.count >= 3 || time.Now().Before(b.next) {
		return false
	}
	b.busy, b.count = true, b.count+1
	c.budgets[key] = b
	return true
}

func (c *exactRetryController) end(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.budgets[key]
	b.busy = false
	kind := exactRetryFailureClass(err)
	transient := kind == "request_timeout" || kind == "provider_rate_limited" ||
		kind == "transport_unavailable" || kind == "provider_unavailable"
	if err == nil || b.count >= 3 || !transient {
		b.terminal = true
	} else {
		b.next = time.Now().Add(time.Duration(b.count*b.count) * time.Minute)
	}
	c.budgets[key] = b
}

// A retry owns the original outbox during its fresh claim. Normal reconciliation
// must never mistake the OLD archived TaskID for this new execution or replay it.
func (p *PersonalRecovery) exactRetryOwnsExecution(
	ctx context.Context, attempt *types.FeishuPersonalAttempt,
) (bool, error) {
	if attempt == nil {
		return false, repository.ErrPersonalIdentity
	}
	if attempt.Status != "processing" {
		return false, nil
	}
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where(
		"scope=? AND op='claimed' AND payload->'source'->>'attempt_id'=? AND payload->>'sealed_generation'=?",
		types.PersonalExactRetryScope, attempt.ID, strconv.FormatInt(attempt.Fence-1, 10),
	).Limit(2).Find(&rows).Error; err != nil {
		return false, err
	}
	matched := 0
	for i := range rows {
		intent, err := decodeExactRetry(&rows[i])
		if err != nil {
			return false, err
		}
		if intent.SealedGeneration+1 == attempt.Fence && intent.Source.KnowledgeID == attempt.LogicalKnowledgeID {
			matched++
		}
	}
	if matched > 1 {
		return false, repository.ErrPersonalFence
	}
	return matched == 1, nil
}

func (p *PersonalRecovery) reconcileExactRetries(ctx context.Context) error {
	var failures []error
	var selected map[string]int64
	if os.Getenv("FEISHU_PERSONAL_EXACT_RETRY") == "1" {
		policy := os.Getenv("FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS")
		if json.Unmarshal([]byte(policy), &selected) != nil || len(selected) > 8 {
			failures = append(failures, repository.ErrPersonalIdentity)
			selected = nil
		}
	}
	for knowledge, generation := range selected {
		_, _, enabled := exactRetryWindow(knowledge)
		if !enabled || p.exactRetries == nil {
			continue
		}
		key := exactRetryKey(knowledge, generation)
		var existing int64
		if err := p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
			Where("scope=? AND scope_id=?", types.PersonalExactRetryScope, key).Count(&existing).Error; err != nil {
			failures = append(failures, err)
			continue
		}
		if existing == 0 && p.exactRetries.begin(key) {
			bounded, stop := context.WithTimeout(ctx, 25*time.Second)
			_, err := p.requestExactRetry(bounded, knowledge, false)
			stop()
			p.exactRetries.end(key, err)
			if err != nil {
				failures = append(failures, err)
			}
		}
	}
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("scope=? AND op IN ?", types.PersonalExactRetryScope,
		[]string{"pending", "dispatched", "claimed"}).
		Order("CASE WHEN op='claimed' THEN 0 ELSE 1 END, id").Limit(64).Find(&rows).Error; err != nil {
		return errors.Join(errors.Join(failures...), err)
	}
	for i := range rows {
		if err := p.reconcileExactRetryTicket(ctx, &rows[i]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (p *PersonalRecovery) reconcileExactRetryTicket(ctx context.Context, row *types.TaskPendingOp) error {
	intent, err := decodeExactRetry(row)
	if err != nil {
		return err
	}
	_, resource, attempt, err := p.exactRetrySubject(ctx, intent.Source.KnowledgeID)
	if err != nil {
		return err
	}
	if row.Op == "claimed" {
		if attempt.ID != intent.Source.AttemptID {
			return repository.ErrPersonalFence
		}
		if attempt.Status == "indexed" {
			return p.finishExactRetryTicket(ctx, row.ID, "completed", "")
		}
		if attempt.Status == "needs_attention" {
			return p.finishExactRetryTicket(ctx, row.ID, "attention", "execution_stopped")
		}
		if attempt.Status == "processing" && resource.Generation == intent.SealedGeneration+1 &&
			attempt.Fence == resource.Generation && attempt.OwnerNonce != "" && attempt.LeaseUntil != nil {
			if attempt.LeaseUntil.After(time.Now()) {
				return nil
			}
			if err := p.repo.AbortExactExecution(ctx, attempt, intent.OriginalOperationID); err != nil {
				return err
			}
			return p.finishExactRetryTicket(ctx, row.ID, "attention", "execution_lease_expired")
		}
		return repository.ErrPersonalFence
	}
	if !intent.NotAfter.After(time.Now()) {
		return p.finishExactRetryTicket(ctx, row.ID, "attention", "approval_window_ended")
	}
	sealed, until, enabled := exactRetryWindow(intent.Source.KnowledgeID)
	if !enabled {
		return nil // Disabled admission never reopens a source or queue.
	}
	if sealed != intent.SealedGeneration || !intent.NotAfter.After(time.Now()) || intent.NotAfter.After(until) {
		return p.finishExactRetryTicket(ctx, row.ID, "attention", "approval_window_ended")
	}
	ks := p.service.knowledge.(*knowledgeService)
	if row.Op == "dispatched" {
		return p.inspectExactRetryDelivery(ctx, ks, row)
	}
	encoded, err := json.Marshal(intent.Task)
	if err != nil {
		return err
	}
	_, err = ks.task.Enqueue(asynq.NewTask(types.TypeDocumentProcess, encoded,
		documentProcessTaskOptions(ks.config, asynq.MaxRetry(0), asynq.TaskID(row.DedupKey),
			asynq.Deadline(intent.NotAfter))...))
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		// Inspection only: archived/completed jobs are NEVER reactivated.
		if err = p.inspectExactRetryDelivery(ctx, ks, row); err != nil {
			return err
		}
	} else if err != nil {
		return err // Keep the durable intent. No new task identity or claim.
	}
	// Lite can finish inline, and Redis can claim before Enqueue returns. Do
	// not overwrite a claimed, completed or sealed state with "dispatched".
	return p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).
		Where("id=? AND scope=? AND op='pending'", row.ID, types.PersonalExactRetryScope).
		Update("op", "dispatched").Error
}

func (p *PersonalRecovery) inspectExactRetryDelivery(
	ctx context.Context, ks *knowledgeService, row *types.TaskPendingOp,
) error {
	inspector, ok := ks.taskInspector.(interfaces.RuntimeTaskInspector)
	if !ok {
		return nil // Lite executes synchronously; its consumer owns the state.
	}
	existing, supported, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, row.DedupKey)
	if err != nil {
		return err
	}
	intent, decodeErr := decodeExactRetry(row)
	if decodeErr != nil {
		return decodeErr
	}
	if !supported || existing == nil || existing.Type != types.TypeDocumentProcess ||
		existing.KnowledgeID != intent.Source.KnowledgeID || existing.TenantID != row.TenantID ||
		existing.KnowledgeBaseID != intent.Source.KnowledgeBaseID {
		return repository.ErrPersonalIdentity
	}
	switch existing.State {
	case types.RuntimeTaskActive, types.RuntimeTaskPending, types.RuntimeTaskScheduled:
		return nil
	case types.RuntimeTaskArchived, types.RuntimeTaskCompleted, types.RuntimeTaskRetry:
		return p.finishExactRetryTicket(ctx, row.ID, "attention", "delivery_not_completed")
	default:
		return repository.ErrPersonalFence
	}
}
