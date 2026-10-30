package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const personalIndexReceiptScope = "personal_index_receipt"

// This receipt confirms the ordinary PG rows, not an immutable serving
// generation or final Wiki success. No model call or index write is performed.
func (p *PersonalRecovery) verifyFirstCreateIndex(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) ([]byte, error) {
	ks := p.service.knowledge.(*knowledgeService)
	k, err := ks.repo.GetKnowledgeByIDOnly(ctx, a.LogicalKnowledgeID)
	if err != nil {
		return nil, err
	}
	var metadata map[string]any
	if json.Unmarshal(k.Metadata, &metadata) != nil || metadata["feishu_mvp_attempt"] != a.ID ||
		metadata["feishu_manifest_sha256"] != a.ManifestHash ||
		k.KnowledgeBaseID != m.KBID ||
		k.EnableStatus != "enabled" {
		return nil, repository.ErrPersonalIdentity
	}
	chunks, err := ks.chunkRepo.ListAllChunksByKnowledgeID(ctx, k.TenantID, k.ID)
	if err != nil {
		return nil, err
	}
	expected := map[string]string{}
	ids := []string{}
	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeText && c.IsEnabled && strings.TrimSpace(c.Content) != "" {
			expected[c.ID] = buildKnowledgeIndexContent(k, c.EmbeddingContent())
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("first_create_no_searchable_text")
	}
	var rows []struct {
		SourceID  string
		ChunkID   string
		Content   string
		Dimension int
	}
	err = p.service.db.WithContext(ctx).
		Table("embeddings").
		Select("source_id, chunk_id, content, dimension").
		Where("knowledge_id = ? AND knowledge_base_id = ? AND source_id IN ? AND "+
			"source_type = ? AND is_enabled = true AND embedding IS NOT NULL AND "+
			"dimension > 0", k.ID, m.KBID, ids, types.ChunkSourceType).
		Find(&rows).
		Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ChunkID == r.SourceID && expected[r.SourceID] == r.Content {
			delete(expected, r.SourceID)
		}
	}
	if len(expected) != 0 {
		return nil, errors.New("first_create_pg_index_rows_incomplete")
	}
	// Exercise the deployed PG keyword operator on the actual document rows.
	// Missing/invalid pg_search indexes produce an error, never a fake receipt.
	var hits int64
	err = p.service.db.WithContext(ctx).
		Raw("SELECT count(*) FROM embeddings WHERE knowledge_id = ? AND "+
			"knowledge_base_id = ? AND source_id IN ? AND content ||| ?", k.ID, m.KBID, ids, rows[0].Content).
		Scan(&hits).
		Error
	if err != nil {
		return nil, err
	}
	if hits == 0 {
		return nil, errors.New("first_create_bm25_readback_empty")
	}
	return json.Marshal(
		map[string]any{
			"payload_version":         1,
			"knowledge_id":            k.ID,
			"attempt_id":              a.ID,
			"manifest_sha256":         a.ManifestHash,
			"file_hash":               k.FileHash,
			"text_chunks":             len(ids),
			"pg_vector_rows_verified": true,
			"bm25_document_readback":  true,
			"verified_at":             time.Now().UTC(),
		},
	)
}

func (p *PersonalRecovery) recordFirstCreateIndex(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	owner *types.FeishuPersonalAttempt,
) error {
	receipt, err := p.verifyFirstCreateIndex(ctx, m, owner)
	if err != nil {
		return err
	}
	return p.mutate(ctx, owner, 0, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
		var count int64
		if err := tx.Model(
			&types.TaskPendingOp{},
		).
			Where(
				"scope = ? AND scope_id = ? AND dedup_key = ?",
				personalIndexReceiptScope,
				a.LogicalKnowledgeID,
				"index-"+
					a.ID,
			).
			Count(
				&count,
			).
			Error; err != nil {
			return err
		}
		if count == 0 {
			if err := tx.Create(
				&types.TaskPendingOp{
					TenantID: a.CallerTenantID,
					TaskType: "knowledge:index_receipt",
					Scope:    personalIndexReceiptScope,
					ScopeID:  a.LogicalKnowledgeID,
					DedupKey: "index-" +
						a.ID,
					Op:         "verified",
					Payload:    receipt,
					EnqueuedAt: time.Now(),
				},
			).
				Error; err != nil {
				return err
			}
		}
		a.Status = "indexed"
		a.LeaseUntil = nil
		a.OwnerNonce = ""
		a.LastErrorCategory = ""
		return tx.Model(&types.FeishuPersonalImport{}).
			Where("id = ?", m.ID).
			Update("status", "indexed").
			Error
	})
}

func (p *PersonalRecovery) firstCreateStatuses(
	ctx context.Context,
	rows []types.FeishuPersonalImport,
) error {
	for i := range rows {
		r := &rows[i]
		r.IndexStatus = "pending"
		r.WikiStatus = "not_started"
		if r.KnowledgeID == "" {
			continue
		}
		var k types.Knowledge
		if err := p.service.db.WithContext(
			ctx,
		).
			Where(
				"id = ? AND knowledge_base_id = ?",
				r.KnowledgeID,
				r.KBID,
			).
			First(
				&k,
			).
			Error; err != nil {
			return err
		}
		var receipts []types.TaskPendingOp
		if err := p.service.db.WithContext(
			ctx,
		).
			Where(
				"scope = ? AND scope_id = ? AND dedup_key = ?",
				personalIndexReceiptScope,
				k.ID,
				"index-"+
					r.AttemptID,
			).
			Limit(
				1,
			).
			Find(
				&receipts,
			).
			Error; err != nil {
			return err
		}
		if len(receipts) == 0 {
			if k.ParseStatus == types.ParseStatusFailed || r.Status == "needs_attention" {
				r.IndexStatus = "attention"
				r.Status = "needs_attention"
			}
			continue
		}
		r.IndexStatus = "verified"
		if r.Status == "needs_attention" {
			r.WikiStatus = "attention"
			continue
		}
		kb, err := p.service.kbs.GetKnowledgeBaseByID(ctx, r.KBID)
		if err != nil {
			return err
		}
		if !kb.IndexingStrategy.WikiEnabled {
			r.WikiStatus = "disabled"
		} else {
			r.WikiStatus = "pending"
			var dead int64
			if err = p.service.db.WithContext(
				ctx,
			).
				Model(
					&types.TaskDeadLetter{},
				).
				Where(
					"task_type = ? AND related_id = ?",
					"wiki:ingest",
					k.ID,
				).
				Count(
					&dead,
				).
				Error; err != nil {
				return err
			}
			if dead > 0 {
				r.WikiStatus = "attention"
				r.Status = "needs_attention"
				continue
			}
			var pending int64
			if err = p.service.db.WithContext(
				ctx,
			).
				Model(
					&types.TaskPendingOp{},
				).
				Where(
					"(task_type = ? AND scope_id = ? AND dedup_key = ?) OR (task_type = ? "+
						"AND scope_id = ?)",

					wikiTaskType,

					r.KBID,

					k.ID,

					wikiFinalizeTaskType,

					r.KBID,
				).
				Count(
					&pending,
				).
				Error; err != nil {
				return err
			}
			if err = p.service.db.WithContext(
				ctx,
			).
				Model(
					&types.TaskDeadLetter{},
				).
				Where(
					"task_type = ? AND scope_id = ?",
					wikiFinalizeTaskType,
					r.KBID,
				).
				Count(
					&dead,
				).
				Error; err != nil {
				return err
			}
			if dead > 0 {
				r.WikiStatus = "attention"
				r.Status = "needs_attention"
				continue
			}
			if k.ParseStatus == types.ParseStatusCompleted && pending == 0 {
				r.WikiStatus = "completed"
			}
		}
		if k.ParseStatus == types.ParseStatusCompleted &&
			(r.WikiStatus == "completed" || r.WikiStatus == "disabled") {
			r.Status = "synced"
		} else if k.ParseStatus == types.ParseStatusFailed {
			r.Status = "needs_attention"
		} else {
			r.Status = "indexed"
		}
	}
	return nil
}

// Exit after the durable index handoff is recovered by readback, not reparse.
func (p *PersonalRecovery) reconcileFirstCreatePublic(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) error {
	var err error
	ctx, err = p.privateWorkerContext(ctx, m, a)
	if err != nil {
		return err
	}
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	return dispatch.WithKnowledgeDispatchLock(ctx, a.LogicalKnowledgeID, func(ctx context.Context) error {
		var n int64
		if err := p.service.db.WithContext(
			ctx,
		).
			Model(
				&types.TaskPendingOp{},
			).
			Where(
				"scope = ? AND scope_id = ? AND dedup_key = ?",

				personalIndexReceiptScope,

				a.LogicalKnowledgeID,

				"index-"+
					a.ID,
			).
			Count(
				&n,
			).
			Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		k, err := p.loadDispatchMetadata(ctx, a.CallerTenantID, m.KBID, m.KnowledgeID)
		if err != nil {
			return err
		}
		if k.AttemptID != a.ID || k.ManifestSHA256 != a.ManifestHash {
			return repository.ErrPersonalIdentity
		}
		op, err := dispatch.GetKnowledgeDispatch(ctx, k.TenantID, a.LogicalKnowledgeID)
		if err != nil {
			return err
		}
		if op.Op != "processed" && op.Op != "completed" {
			return nil
		}
		owner, err := p.repo.Claim(ctx, a.ID, p.lease)
		if errors.Is(err, repository.ErrPersonalBusy) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = p.authorize(ctx, m, owner); err != nil {
			return err
		}
		if err = p.recordFirstCreateIndex(ctx, m, owner); err != nil {
			return errors.Join(err, p.repo.Invalidate(ctx, a.ID, "pg_index_receipt_unconfirmed"))
		}
		return nil
	})
}
