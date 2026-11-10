package repository

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func ReadWikiDocumentSkipSummary(ctx context.Context, repo interfaces.WikiPageRepository, kb string) (map[string]int64, error) {
	counts := map[string]int64{}
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return counts, nil
	}
	var anchor types.KnowledgeBase
	if err := r.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, kb)).Where("id=?", kb).First(&anchor).Error; err != nil {
		return nil, err
	}
	var records []types.TaskPendingOp
	if err := r.db.WithContext(ctx).Where("tenant_id=? AND scope=? AND scope_id=? AND task_type='wiki:document_skip' AND op='skipped'", anchor.TenantID, wikiDocumentSkipScope, kb).Where("EXISTS(SELECT 1 FROM knowledges k WHERE k.id=task_pending_ops.dedup_key AND k.tenant_id=task_pending_ops.tenant_id AND k.knowledge_base_id=task_pending_ops.scope_id AND k.deleted_at IS NULL)").Limit(2049).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	for _, row := range records {
		var payload struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, err
		}
		counts[payload.Reason]++
	}
	return counts, nil
}
