package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Re-enter the original explicit admission only after a known parser outage has
// recovered. Prepare remains the atomic zero-output/ownership review boundary;
// this never resets an attempt, fence, cursor or publication approval.
func (s *FeishuPersonalService) requeueUnavailableParserBatches(ctx context.Context) error {
	ks, ok := s.knowledge.(*knowledgeService)
	if !ok || ks.documentReader == nil || !ks.documentReader.IsConnected() || s.recovery == nil ||
		!s.db.Migrator().HasTable(&types.KnowledgeProcessingSpan{}) {
		return nil
	}
	var items []types.FeishuBatchItem
	if err := s.db.WithContext(ctx).Where(
		"status = ? AND error_category = ? AND import_id <> ? AND retry_count < 3"+
			" AND (lease_until IS NULL OR lease_until < ?)",
		"failed", "parse_or_index_result_unconfirmed", "", time.Now()).
		Order("updated_at,id").Limit(2).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		var batch types.FeishuBatch
		if err := s.db.WithContext(ctx).First(&batch, "id = ?", item.BatchID).Error; err != nil {
			return err
		}
		if batch.AuthMode != types.FeishuUserOAuth || batch.PublicationConsentAt == nil ||
			!batch.PublicationConsentAt.Add(24*time.Hour).After(time.Now()) || item.SharingApprovalID == nil {
			continue
		}
		fresh, err := s.batchContext(ctx, &batch)
		if err != nil {
			continue
		}
		fresh = batchPublicationContext(fresh, &item)
		var imported types.FeishuPersonalImport
		if err = s.db.WithContext(fresh).First(&imported, "id = ? AND user_id = ? AND kb_id = ?",
			item.ImportID, batch.OwnerID, batch.KBID).Error; err != nil {
			continue
		}
		var state types.FeishuPersonalResource
		if err = s.db.WithContext(fresh).First(&state, "import_id = ?", imported.ID).Error; err != nil {
			continue
		}
		m, resource, attempt, err := s.recovery.repo.Load(fresh, state.CurrentAttemptID)
		if err != nil || resource.ActiveVersionID != "" || resource.CurrentAttemptID != attempt.ID ||
			m.ID != imported.ID || m.UserID != batch.OwnerID || m.KBID != batch.KBID ||
			attempt.CallerTenantID != batch.TenantID || attempt.AppConfigVersion != batch.ConfigVersion ||
			connectionID(
				batch.AuthMode,
				m.UserID,
				m.AppID,
				m.OpenID,
				attempt.GrantEpoch,
			) != batch.ConnectionID ||
			m.Status != "needs_attention" || attempt.Status != "needs_attention" ||
			attempt.LastErrorCategory != "parse_or_index_result_unconfirmed" || m.KnowledgeID == "" ||
			m.KnowledgeID != resource.LogicalKnowledgeID || attempt.LeaseUntil != nil {
			continue
		}
		if _, err = s.recovery.authorize(fresh, m, attempt); err != nil {
			continue
		}
		if _, err = s.recovery.store.read(attempt); err != nil {
			continue
		}
		var knowledge types.Knowledge
		if err = s.db.WithContext(fresh).First(&knowledge, "id = ?", m.KnowledgeID).Error; err != nil {
			continue
		}
		if knowledge.EnableStatus == "enabled" ||
			(knowledge.ParseStatus != types.ParseStatusProcessing &&
				knowledge.ParseStatus != types.ParseStatusPending &&
				knowledge.ParseStatus != types.ParseStatusFailed) {
			continue
		}
		var stage types.KnowledgeProcessingSpan
		if err = s.db.WithContext(fresh).Where("knowledge_id = ? AND kind = ? AND name = ?",
			knowledge.ID, types.SpanKindStage, types.StageDocReader).
			Order("attempt DESC,updated_at DESC,id DESC").First(&stage).Error; err != nil ||
			stage.Status != types.SpanStatusFailed || stage.ErrorCode != "DOCREADER_UNAVAILABLE" {
			continue
		}
		var chunks, indexes, receipts int64
		if err = s.db.WithContext(fresh).Unscoped().Model(&types.Chunk{}).
			Where("knowledge_id = ?", knowledge.ID).Count(&chunks).Error; err != nil {
			return err
		}
		if err = s.db.WithContext(fresh).Table("embeddings").
			Where("knowledge_id = ?", knowledge.ID).Count(&indexes).Error; err != nil {
			return err
		}
		if err = s.db.WithContext(fresh).Model(&types.TaskPendingOp{}).
			Where("scope = ? AND scope_id = ?", "personal_index_receipt", knowledge.ID).
			Count(&receipts).Error; err != nil {
			return err
		}
		if chunks != 0 || indexes != 0 || receipts != 0 {
			continue
		}
		// No first-create state changes here. The original pipeline rechecks live
		// identity/revision and Prepare locks/counts all outputs again before retry.
		if err = s.db.WithContext(fresh).Model(&types.FeishuBatchItem{}).
			Where("id = ? AND status = ? AND import_id = ? AND retry_count = ?"+
				" AND (lease_until IS NULL OR lease_until < ?)",
				item.ID, "failed", item.ImportID, item.RetryCount, time.Now()).
			Updates(map[string]any{
				"status": "pending", "error_category": "", "updated_at": time.Now(),
				"retry_count": gorm.Expr("retry_count + 1"),
			}).Error; err != nil {
			return err
		}
	}
	return nil
}
