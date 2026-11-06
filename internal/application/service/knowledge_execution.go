package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// ProcessingExecutions reports actual ownership separately from poll/progress
// timestamps. Personal protocol state overrides a generic durable queue record.
func (h *HousekeepingService) ProcessingExecutions(
	ctx context.Context, ids []string,
) (map[string]types.KnowledgeExecutionState, error) {
	queued, err := h.QueuedWork(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]types.KnowledgeExecutionState, len(ids))
	for _, id := range ids {
		state := "waiting"
		if queued[id] {
			state = "queued"
		}
		out[id] = types.KnowledgeExecutionState{State: state}
	}
	if probe, ok := h.inspector.(interface {
		KnowledgeExecutionLeases(context.Context, []string) (map[string]time.Time, error)
	}); ok {
		leases, err := probe.KnowledgeExecutionLeases(ctx, ids)
		if err != nil {
			return nil, err
		}
		for id, until := range leases {
			out[id] = types.KnowledgeExecutionState{State: "running", LeaseUntil: &until}
		}
	}
	var attempts []types.FeishuPersonalAttempt
	if err := h.db.WithContext(ctx).Table("feishu_personal_attempts a").Select("a.*").
		Joins(`JOIN feishu_personal_resources r ON r.current_attempt_id=a.id AND
(r.generation=a.fence OR (r.generation=a.fence+1 AND a.status='needs_attention'
AND a.owner_nonce='' AND a.lease_until IS NULL))`).
		Where("a.logical_knowledge_id IN ?", ids).Find(&attempts).Error; err != nil {
		return nil, err
	}
	var now time.Time
	if err := h.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		return nil, err
	}
	for _, a := range attempts {
		state := types.KnowledgeExecutionState{State: "queued"}
		if a.Status == "processing" && a.OwnerNonce != "" && a.LeaseUntil != nil && a.LeaseUntil.After(now) {
			state.State, state.LeaseUntil = "running", a.LeaseUntil
		} else if a.LastErrorCategory != "" {
			state.State, state.RetryAfter = "blocked", a.RetryAfter
			switch a.LastErrorCategory {
			case "exact_document_resume_prepared":
				state.State, state.Reason = "retrying", "controlled_resume"
			case "preflight_transient":
				state.State, state.Reason = "retrying", "temporary_failure"
			case "embedded_dependency_unsupported":
				state.Reason = "document_format_unsupported"
			case "authorization_revalidation_failed":
				state.Reason = "source_authorization_required"
			default:
				state.Reason = "source_verification_required"
			}
		} else if a.Status == "processing" {
			state.State = "stalled"
		}
		out[a.LogicalKnowledgeID] = state
	}
	return out, nil
}
