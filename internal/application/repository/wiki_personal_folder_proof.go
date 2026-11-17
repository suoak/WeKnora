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

// WikiPersonalFolderPlan proves an unchanged publication against its original
// filing metadata. No body, folder, parser state or existing page is rewritten.
type WikiPersonalFolderPlan struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
	OriginalFolder  string `json:"original_folder"`
	Snapshot        string `json:"snapshot"`
	Applied         bool   `json:"applied"`
}

// PrepareWikiPersonalFolderProof requires the caller's normal Wiki lease.
// The proposed original path must reproduce the original full-row digest;
// unchanged source identities, all chunks and all vectors remain mandatory.
func PrepareWikiPersonalFolderProof(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, id, original, expected string, apply bool,
) (*WikiPersonalFolderPlan, error) {
	if !WikiDocumentSelectionForKB(kb) || tenant == 0 || id == "" ||
		types.NormalizeKnowledgeFolderPath(original) != original {
		return nil, ErrWikiMCPUnverified
	}
	plan := &WikiPersonalFolderPlan{
		TenantID: tenant, KnowledgeBaseID: kb, KnowledgeID: id, OriginalFolder: original,
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if apply {
			var anchor types.KnowledgeBase
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id=? AND tenant_id=?", kb, tenant).First(&anchor).Error; err != nil {
				return err
			}
			var row struct{ ID string }
			if err := tx.Table("knowledges").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id=? AND tenant_id=? AND knowledge_base_id=?", id, tenant, kb).Take(&row).Error; err != nil {
				return err
			}
		}
		var receipt types.TaskPendingOp
		query := tx.Where("scope=? AND scope_id=? AND tenant_id=? AND op='published'",
			types.PersonalExactReceiptScope, id, tenant)
		if apply {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&receipt).Error; err != nil {
			return err
		}
		var proof types.PersonalExactReceipt
		if json.Unmarshal(receipt.Payload, &proof) != nil ||
			(proof.KnowledgeFolderPath != nil && *proof.KnowledgeFolderPath != original) {
			return ErrWikiMCPUnverified
		}
		publication, err := wikiPersonalPublicationAtFolder(tx, tenant, kb, id, &original)
		if err != nil {
			return err
		}
		var current string
		if err := tx.Raw("SELECT encode(sha256(convert_to(to_jsonb(k)::text,'UTF8')),'hex') "+
			"FROM knowledges k WHERE id=? AND tenant_id=? AND knowledge_base_id=?", id, tenant, kb).
			Scan(&current).Error; err != nil {
			return err
		}
		plan.Snapshot = wikiDigest([]any{tenant, kb, id, original, receipt.ID, receipt.Payload, current})
		if !apply {
			return nil
		}
		if expected == "" || expected != plan.Snapshot {
			return errors.New("personal folder proof snapshot changed; preview again")
		}
		if proof.KnowledgeFolderPath == nil {
			value, _ := json.Marshal(original)
			updated := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='published' AND payload::jsonb=?::jsonb",
				receipt.ID, string(receipt.Payload)).UpdateColumn("payload",
				gorm.Expr("jsonb_set(payload::jsonb,'{knowledge_folder_path}',?::jsonb)", string(value)))
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrWikiMCPInputsChanged
			}
			fresh, err := WikiPersonalPublication(tx, tenant, kb, id)
			if err != nil || fresh != publication {
				return ErrWikiMCPInputsChanged
			}
			payload, _ := json.Marshal(map[string]any{
				"receipt_id": receipt.ID, "previous_payload_sha256": wikiDigest(receipt.Payload),
				"publication_sha256": publication, "original_folder": original,
			})
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:folder-proof", Scope: "personal_exact_folder_proof_v1",
				ScopeID: id, Op: "verified", DedupKey: proof.AttemptID, Payload: payload, EnqueuedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
