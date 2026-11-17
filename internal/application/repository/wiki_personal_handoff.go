package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalWikiHandoffScope = "personal_wiki_handoff"

// WikiPersonalHandoffCandidate contains only dispatch identities, never source text.
type WikiPersonalHandoffCandidate struct {
	ReceiptID       int64
	TenantID        uint64
	KnowledgeBaseID string
	KnowledgeID     string
}

// WikiPersonalHandoffCandidates pages monotonically so a refused old receipt
// cannot starve later publications. A handoff is recorded per publication.
func WikiPersonalHandoffCandidates(
	ctx context.Context, db *gorm.DB, after int64, limit int,
) ([]WikiPersonalHandoffCandidate, error) {
	if db.Name() != "postgres" || !WikiDocumentSelectionEnabled() {
		return nil, nil
	}
	if limit < 1 || limit > 128 {
		return nil, errors.New("invalid personal Wiki handoff scan limit")
	}
	var rows []WikiPersonalHandoffCandidate
	err := db.WithContext(ctx).Table("task_pending_ops r").
		Select("r.id AS receipt_id,k.tenant_id,k.knowledge_base_id,k.id AS knowledge_id").
		Joins("JOIN knowledges k ON k.id=r.scope_id AND k.tenant_id=r.tenant_id AND k.deleted_at IS NULL").
		Joins("JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id AND "+
			"kb.tenant_id=k.tenant_id AND kb.deleted_at IS NULL").
		Where("r.scope=? AND r.op='published' AND r.id>? AND k.channel='feishu_personal' AND "+
			"k.parse_status='completed'", types.PersonalExactReceiptScope, after).
		Where("kb.indexing_strategy->>'wiki_enabled'='true'").
		Where("NOT EXISTS(SELECT 1 FROM task_pending_ops h WHERE h.scope=? AND h.scope_id=k.id AND "+
			"h.tenant_id=k.tenant_id AND h.dedup_key=r.payload->>'attempt_id')", personalWikiHandoffScope).
		Order("r.id").Limit(limit).Scan(&rows).Error
	return rows, err
}

// EnsureWikiPersonalHandoff atomically records a verified publication and its
// durable ingest intent. It never reopens parsing or changes receipt-bound rows.
// The normal Wiki worker independently checks source/model/page authority.
func EnsureWikiPersonalHandoff(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, knowledge string, expected ...string,
) (bool, error) {
	if !WikiDocumentSelectionForKB(kb) {
		return false, nil
	}
	queued := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", kb, tenant).
			First(&anchor).Error; err != nil {
			return err
		}
		if !anchor.IsWikiEnabled() || anchor.IsTemporary || anchor.Type != "document" {
			return nil
		}
		// Reject private spaces, changed source bindings and unreceipted output
		// before creating a durable work item, including during repair/backfill.
		selection, _, err := selectWikiDocumentInputs(tx, kb, tenant)
		if err != nil {
			return err
		}
		if !selection.Eligible[knowledge] {
			return ErrWikiMCPUnverified
		}
		digest, err := WikiPersonalPublication(tx, tenant, kb, knowledge)
		if err != nil {
			return err
		}
		if len(expected) > 1 || len(expected) == 1 && expected[0] != digest {
			return errors.New("personal Wiki publication changed; preview again")
		}
		var receipt types.TaskPendingOp
		if err = tx.Where("scope=? AND scope_id=? AND tenant_id=? AND op='published'",
			types.PersonalExactReceiptScope, knowledge, tenant).First(&receipt).Error; err != nil {
			return err
		}
		var proof types.PersonalExactReceipt
		if json.Unmarshal(receipt.Payload, &proof) != nil || proof.AttemptID == "" {
			return ErrWikiMCPUnverified
		}
		var count int64
		if err = tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=? AND tenant_id=? AND dedup_key=?",
			personalWikiHandoffScope, knowledge, tenant, proof.AttemptID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		// Adopt an existing intent without overwriting its attempt, payload, claim
		// or retry counter. A lost trigger is recovered independently below.
		if err = tx.Model(&types.TaskPendingOp{}).Where("scope='knowledge_base' AND task_type='wiki:ingest' AND "+
			"op='ingest' AND scope_id=? AND tenant_id=? AND dedup_key=?", kb, tenant, knowledge).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			var attempt int
			if err = tx.Table("knowledge_processing_spans").Select("COALESCE(MAX(attempt),0)").
				Where("knowledge_id=?", knowledge).Scan(&attempt).Error; err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{
				"op": "ingest", "knowledge_id": knowledge, "attempt": attempt, "language": "zh-CN",
			})
			if err = tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: kb,
				Op: "ingest", DedupKey: knowledge, Payload: payload, EnqueuedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		payload, _ := json.Marshal(map[string]any{
			"receipt_id": receipt.ID, "publication_sha256": digest, "knowledge_base_id": kb,
		})
		if err = tx.Create(&types.TaskPendingOp{
			TenantID: tenant, TaskType: "wiki:handoff", Scope: personalWikiHandoffScope, ScopeID: knowledge,
			Op: "queued", DedupKey: proof.AttemptID, Payload: payload, EnqueuedAt: time.Now(),
		}).Error; err != nil {
			return err
		}
		queued = true
		return nil
	})
	return queued && err == nil, err
}

// WikiPersonalHandoffPlan binds a repair to current verified publications.
type WikiPersonalHandoffPlan struct {
	TenantID        uint64            `json:"tenant_id"`
	KnowledgeBaseID string            `json:"knowledge_base_id"`
	Publications    map[string]string `json:"publications"`
	Snapshot        string            `json:"snapshot"`
	Applied         bool              `json:"applied"`
	Queued          int               `json:"queued"`
}

// PrepareWikiPersonalHandoff is the bounded operator preview/apply path. The
// caller holds the normal Wiki generation lease. No pages, parser rows or
// historical operations are deleted or declared successful here.
func PrepareWikiPersonalHandoff(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiPersonalHandoffPlan, error) {
	if !WikiDocumentSelectionForKB(kb) {
		return nil, errors.New("personal Wiki selection is not active")
	}
	plan := &WikiPersonalHandoffPlan{TenantID: tenant, KnowledgeBaseID: kb, Publications: map[string]string{}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if apply {
			var anchor types.KnowledgeBase
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", kb, tenant).
				First(&anchor).Error; err != nil {
				return err
			}
			if !anchor.IsWikiEnabled() {
				return errors.New("Wiki is disabled")
			}
		}
		selection, err := PreviewWikiDocumentSelection(ctx, tx, tenant, kb)
		if err != nil {
			return err
		}
		for _, id := range selection.EligibleDocuments {
			var channel string
			if err := tx.Table("knowledges").Select("channel").Where("id=? AND tenant_id=? AND "+
				"knowledge_base_id=? AND deleted_at IS NULL", id, tenant, kb).Scan(&channel).Error; err != nil {
				return err
			}
			if channel != "feishu_personal" {
				continue
			}
			digest, err := WikiPersonalPublication(tx, tenant, kb, id)
			if err != nil {
				return err
			}
			plan.Publications[id] = digest
		}
		plan.Snapshot = wikiDigest([]any{selection.Snapshot, plan.Publications})
		if !apply {
			return nil
		}
		if expected == "" || expected != plan.Snapshot {
			return errors.New("personal Wiki handoff snapshot changed; preview again")
		}
		for id, digest := range plan.Publications {
			queued, err := EnsureWikiPersonalHandoff(ctx, tx, tenant, kb, id, digest)
			if err != nil {
				return err
			}
			if queued {
				plan.Queued++
			}
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
