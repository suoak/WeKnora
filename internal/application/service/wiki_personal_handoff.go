package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
)

func (p *PersonalRecovery) handoffPublishedPersonalWiki(ctx context.Context, tenant uint64, kb, knowledge string) error {
	queued, err := repository.EnsureWikiPersonalHandoff(ctx, p.service.db, tenant, kb, knowledge)
	if err != nil || !queued {
		return err
	}
	return enqueueWikiIngestTrigger(ctx, p.service.knowledge.(*knowledgeService).task, tenant, kb)
}

// The parser lifecycle is complete for personal publications, so stalled-parse
// sweeps cannot recover this handoff. Page through receipts independently and
// also re-arm already-persisted work when Redis delivery was lost.
func (h *HousekeepingService) recoverPersonalWikiHandoffs(ctx context.Context) {
	if h.task == nil || !wikiDocumentSelectionEnabled() || ctx.Err() != nil {
		return
	}
	h.kickMu.Lock()
	after := h.personalWikiCursor
	h.kickMu.Unlock()
	rows, err := repository.WikiPersonalHandoffCandidates(ctx, h.db, after, 64)
	if err != nil {
		logger.Warnf(ctx, "[Housekeeping] Personal Wiki handoff scan failed: %v", err)
		return
	}
	next := int64(0)
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		next = row.ReceiptID
		if _, err := repository.EnsureWikiPersonalHandoff(ctx, h.db, row.TenantID, row.KnowledgeBaseID, row.KnowledgeID); err != nil {
			logger.Warnf(ctx, "[Housekeeping] Personal Wiki handoff remains unverified for %s: %v", row.KnowledgeID, err)
		}
	}
	h.kickMu.Lock()
	h.personalWikiCursor = next
	h.kickMu.Unlock()
	var lanes []struct {
		TenantID uint64
		ScopeID  string
	}
	err = h.db.WithContext(ctx).Table("task_pending_ops p").Select("p.tenant_id,p.scope_id").
		Joins("JOIN knowledge_bases kb ON kb.id=p.scope_id AND kb.tenant_id=p.tenant_id AND kb.deleted_at IS NULL").
		Joins("JOIN tenants t ON t.id=p.tenant_id AND t.deleted_at IS NULL AND t.status='active'").
		Where("p.scope='knowledge_base' AND p.task_type='wiki:ingest' AND p.op='ingest'").
		Where("kb.indexing_strategy->>'wiki_enabled'='true'").
		Where("EXISTS(SELECT 1 FROM task_pending_ops h WHERE h.scope='personal_wiki_handoff' AND h.scope_id=p.dedup_key AND h.tenant_id=p.tenant_id)").
		Group("p.tenant_id,p.scope_id").Order("MIN(p.id)").Limit(64).Scan(&lanes).Error
	if err != nil {
		logger.Warnf(ctx, "[Housekeeping] Personal Wiki trigger scan failed: %v", err)
		return
	}
	for _, lane := range lanes {
		if !repository.WikiDocumentSelectionForKB(lane.ScopeID) {
			continue
		}
		if err := enqueueWikiIngestTrigger(ctx, h.task, lane.TenantID, lane.ScopeID); err != nil {
			logger.Warnf(ctx, "[Housekeeping] Personal Wiki trigger recovery failed: %v", err)
		}
	}
}
