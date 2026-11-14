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
	"github.com/hibiken/asynq"
)

const wikiBlockRecoveryInterval = 10 * time.Minute

// Refused triggers are archived while SQL operations survive. Completed
// documents never enter the stalled-row sweep, so wake cooled-down lanes
// independently. Only consumers may revalidate sources/pages or settle work.
func (h *HousekeepingService) rearmExpiredWikiBlocks(ctx context.Context) {
	if h.task == nil || ctx.Err() != nil {
		return
	}
	var lanes []struct {
		TenantID uint64
		ScopeID  string
		TaskType string
		Head     int64
		Payload  string
	}
	err := h.db.WithContext(ctx).Table("task_pending_ops AS p").Select(
		"p.tenant_id,p.scope_id,p.task_type,MIN(p.id) AS head,b.payload").
		Joins("JOIN knowledge_bases kb ON kb.id=p.scope_id AND kb.tenant_id=p.tenant_id AND kb.deleted_at IS NULL").
		Joins("JOIN tenants t ON t.id=p.tenant_id AND t.deleted_at IS NULL AND t.status='active'").
		Joins("JOIN task_pending_ops b ON b.scope='wiki_processing_state' AND b.task_type='wiki:state'"+
			" AND b.scope_id=p.scope_id AND b.tenant_id=p.tenant_id AND b.op='blocked'").
		Where("p.scope=? AND p.task_type IN ?", types.TaskScopeKnowledgeBase,
			[]string{types.TypeWikiIngest, types.TypeWikiFinalize}).
		Where("NOT EXISTS (SELECT 1 FROM task_pending_ops busy WHERE busy.scope=p.scope"+
			" AND busy.scope_id=p.scope_id AND busy.tenant_id=p.tenant_id AND busy.claimed_at IS NOT NULL"+
			" AND busy.task_type IN ?)", []string{types.TypeWikiIngest, types.TypeWikiFinalize}).
		Group("p.tenant_id,p.scope_id,p.task_type,b.payload").Order("MIN(p.id)").Scan(&lanes).Error
	if err != nil {
		logger.Warnf(ctx, "[Housekeeping] Wiki blocked-lane scan failed: %v", err)
		return
	}
	now := time.Now()
	h.kickMu.Lock()
	defer h.kickMu.Unlock()
	if h.wikiKicks == nil {
		h.wikiKicks = make(map[string]time.Time)
	}
	for key, last := range h.wikiKicks {
		if now.Sub(last) >= wikiBlockRecoveryInterval {
			delete(h.wikiKicks, key)
		}
	}
	for _, lane := range lanes {
		if ctx.Err() != nil {
			return
		}
		var block repository.WikiProcessingBlock
		if json.Unmarshal([]byte(lane.Payload), &block) != nil || block.Reason == "" ||
			block.RetryAfter.IsZero() || now.Before(block.RetryAfter) {
			continue
		}
		if block.Reason != "source_verification_required" && block.Reason != "verified_regeneration_required" {
			continue
		}
		key := "blocked:" + lane.ScopeID + ":" + lane.TaskType
		if last := h.wikiKicks[key]; !last.IsZero() && now.Sub(last) < wikiBlockRecoveryInterval {
			continue
		}
		payload, marshalErr := json.Marshal(WikiIngestPayload{
			TenantID: lane.TenantID, KnowledgeBaseID: lane.ScopeID,
			Language: WikiPendingLanguage(ctx, h.db, lane.TenantID, lane.ScopeID),
		})
		if marshalErr != nil {
			continue
		}
		// Replica-stable IDs collapse duplicate wake-ups. The next bucket can
		// recover an archived trigger without deleting its original evidence.
		id := fmt.Sprintf("wiki-block-recovery-%s-%s-%d-%d", lane.TaskType, lane.ScopeID,
			lane.Head, now.Unix()/int64(wikiBlockRecoveryInterval/time.Second))
		timeout := 60 * time.Minute
		if lane.TaskType == types.TypeWikiFinalize {
			timeout = 30 * time.Minute
		}
		_, enqueueErr := h.task.Enqueue(asynq.NewTask(lane.TaskType, payload,
			asynq.Queue(types.QueueWiki), asynq.TaskID(id), asynq.MaxRetry(10), asynq.Timeout(timeout)))
		if enqueueErr != nil && !errors.Is(enqueueErr, asynq.ErrTaskIDConflict) &&
			!errors.Is(enqueueErr, asynq.ErrDuplicateTask) {
			logger.Warnf(ctx, "[Housekeeping] Wiki blocked-lane wake-up failed for %s: %v", lane.ScopeID, enqueueErr)
			continue
		}
		h.wikiKicks[key] = now
	}
}
