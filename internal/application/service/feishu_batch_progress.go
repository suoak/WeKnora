package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type batchQueueProgress struct {
	checked bool
	ids     map[string]struct{}
	err     error
}

// batchItemProgress reads existing protocol receipts and parser spans. Polling
// never writes a heartbeat or moves a job. Queue uncertainty cannot cancel work.
func (s *FeishuPersonalService) batchItemProgress(ctx context.Context, item *types.FeishuBatchItem,
	m *types.FeishuPersonalImport, accepted time.Time, queue *batchQueueProgress,
) error {
	item.Stage = "queued"
	item.LastProgressAt = &accepted
	item.LeaseActive = item.LeaseUntil != nil && item.LeaseUntil.After(time.Now())
	if item.Status == "processing" {
		item.Stage = "checking"
	}
	if item.Status == "blocked" || item.Status == "failed" {
		item.Stage = "needs_attention"
	}
	if m == nil {
		return nil
	}
	item.KnowledgeID, item.WikiStatus = m.KnowledgeID, m.WikiStatus
	item.IndexVerified = m.IndexStatus == "verified"
	if !item.IndexVerified && (item.Status == "succeeded" || item.Status == "existing") {
		item.Status = "dispatched"
	}
	if !m.UpdatedAt.IsZero() {
		value := m.UpdatedAt
		item.LastProgressAt = &value
	}
	var resource types.FeishuPersonalResource
	if e := s.db.WithContext(ctx).Where("import_id = ?", m.ID).First(&resource).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			item.Stage = "preparing"
			return nil
		}
		return e
	}
	var a types.FeishuPersonalAttempt
	if e := s.db.WithContext(ctx).Where("id = ?", resource.CurrentAttemptID).First(&a).Error; e != nil {
		return e
	}
	item.AttemptRetryCount, item.RetryAfter = a.RetryCount, a.RetryAfter
	value := a.UpdatedAt
	item.LastActivityAt = &value
	item.LeaseActive = item.LeaseActive || a.LeaseUntil != nil && a.LeaseUntil.After(time.Now())
	item.Stage = "preparing"
	if a.Status == "processing" {
		item.Stage = "fetching"
	}
	if a.Status == "recovery" {
		item.Stage = "retry_wait"
	}
	if a.ManifestSize > 0 {
		item.Stage = "downloaded"
	}
	if a.LastErrorCategory != "" {
		item.ErrorCategory = a.LastErrorCategory
	}
	if a.Status == "needs_attention" || m.Status == "needs_attention" {
		item.Status, item.Stage = "failed", "needs_attention"
	}
	if m.KnowledgeID != "" {
		var k types.Knowledge
		if e := s.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ?", m.KnowledgeID, m.KBID).First(&k).Error; e != nil {
			return e
		}
		item.ParseStatus = k.ParseStatus
		failureCategory := "parse_or_index_failed"
		if k.CreatedAt.After(*item.LastProgressAt) {
			t := k.CreatedAt
			item.LastProgressAt = &t
		}
		item.Stage = "parse_queued"
		if k.ParseStatus == types.ParseStatusProcessing {
			item.Stage = "parsing"
		}
		if k.ParseStatus == types.ParseStatusFinalizing {
			item.Stage = "finalizing"
		}
		if e := s.db.WithContext(ctx).Model(&types.Chunk{}).Where("knowledge_id = ?", k.ID).Count(&item.ChunkCount).Error; e != nil {
			return e
		}
		if e := s.db.WithContext(ctx).Table("embeddings").Where("knowledge_id = ? AND embedding IS NOT NULL", k.ID).Count(&item.VectorCount).Error; e != nil {
			return e
		}
		if s.db.Migrator().HasTable(&types.KnowledgeProcessingSpan{}) {
			var spans []types.KnowledgeProcessingSpan
			if e := s.db.WithContext(ctx).Select("name,kind,status,attempt,started_at,finished_at,updated_at").
				Where("knowledge_id = ? AND kind = ? AND attempt = "+
					"(SELECT MAX(attempt) FROM knowledge_processing_spans WHERE knowledge_id = ?)",
					k.ID, types.SpanKindStage, k.ID).
				Order("attempt DESC, updated_at DESC").Limit(5).Find(&spans).Error; e != nil {
				return e
			}
			for _, span := range spans {
				if span.Status == types.SpanStatusFailed {
					failureCategory = "parse_failed"
					if span.Name == types.StageEmbedding {
						failureCategory = "index_failed"
					}
				}
				if item.LastActivityAt == nil || span.UpdatedAt.After(*item.LastActivityAt) {
					t := span.UpdatedAt
					item.LastActivityAt = &t
				}
				if span.FinishedAt != nil && span.Status == types.SpanStatusDone &&
					span.FinishedAt.After(*item.LastProgressAt) {
					t := *span.FinishedAt
					item.LastProgressAt = &t
				}
			}
			for _, span := range spans {
				if span.Status == types.SpanStatusRunning {
					item.Stage = span.Name
					break
				}
			}
		}
		if k.ParseStatus == types.ParseStatusFailed || k.ParseStatus == types.ParseStatusCancelled {
			item.Status, item.Stage, item.ErrorCategory = "failed", "needs_attention", failureCategory
		}
	}
	// Success is a verified index receipt plus completed publication/Wiki.
	if item.IndexVerified && item.ParseStatus == types.ParseStatusCompleted &&
		(m.Status == "synced" || m.Status == "completed" || m.Status == "source_managed") {
		item.Status, item.Stage, item.ErrorCategory = "succeeded", "completed", ""
		var receipt types.TaskPendingOp
		if e := s.db.WithContext(ctx).Where("scope = ? AND scope_id = ? AND op = ?",
			personalIndexReceiptScope, m.KnowledgeID, "verified").Order("enqueued_at DESC").First(&receipt).Error; e == nil {
			t := receipt.EnqueuedAt
			item.LastProgressAt = &t
		}
	} else if item.Status == "succeeded" || item.Status == "existing" {
		item.Status = "dispatched"
	}
	if item.Stage != "completed" && item.Stage != "needs_attention" &&
		time.Since(*item.LastProgressAt) > 15*time.Minute &&
		!item.LeaseActive &&
		(item.RetryAfter == nil || !item.RetryAfter.After(time.Now())) {
		if ks, ok := s.knowledge.(*knowledgeService); ok && ks.taskInspector != nil {
			if !queue.checked {
				probe, cancel := context.WithTimeout(ctx, 2*time.Second)
				queue.ids, queue.err = ks.taskInspector.QueuedKnowledgeIDs(probe)
				cancel()
				queue.checked = true
			}
			if queue.err == nil {
				_, candidate := queue.ids[a.CandidateVersionID]
				_, formal := queue.ids[a.LogicalKnowledgeID]
				item.QueueActive = candidate || formal
				fresh := item.LastActivityAt != nil && time.Since(*item.LastActivityAt) < 2*time.Minute
				item.Stalled = !item.QueueActive && !fresh
			}
		}
	}
	return nil
}
