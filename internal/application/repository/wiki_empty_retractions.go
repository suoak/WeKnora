package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WikiEmptyRetractionPlan proves a cleanup has no remaining targets. It grants
// no content authority and never queues generation or certifies old pages.
type WikiEmptyRetractionPlan struct {
	Mode              string `json:"mode"`
	TenantID          uint64 `json:"tenant_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	PendingOperations int    `json:"pending_operations"`
	CurrentPages      int    `json:"current_pages"`
	Snapshot          string `json:"snapshot"`
	Applied           bool   `json:"applied"`
}

// PrepareWikiEmptyRetractions is the operator's preview/apply path for deleted
// documents whose retractions never had page/folder targets and have no current
// Wiki references. Callers hold the existing renewable Wiki generation lease.
// Original rows/payloads survive in the existing regeneration archive scope.
func PrepareWikiEmptyRetractions(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiEmptyRetractionPlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("tenant, KB and apply snapshot are required")
	}
	var plan *WikiEmptyRetractionPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor struct{ ID string }
		if err := tx.Table("knowledge_bases").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where(`id=? AND tenant_id=? AND deleted_at IS NULL AND type='document'
 AND EXISTS(SELECT 1 FROM tenants WHERE id=? AND status='active' AND deleted_at IS NULL)`,
			kb, tenant, tenant).Take(&anchor).Error; err != nil {
			return err
		}
		var private int64
		if err := tx.Table("feishu_private_kbs").Where("kb_id=?", kb).Count(&private).Error; err != nil {
			return err
		}
		if private != 0 {
			return types.ErrPrivateKnowledgeBase
		}
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		var pending []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope=? AND scope_id=?", types.TaskScopeKnowledgeBase, kb).
			Order("id").Limit(1001).Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) == 0 || len(pending) > 1000 {
			return errors.New("empty retraction retirement requires 1..1000 original operations")
		}
		type document struct {
			ID        string
			DeletedAt *time.Time
		}
		documents := make([]document, 0, len(pending))
		targets := make(map[string]bool, len(pending))
		for _, op := range pending {
			var payload struct {
				Op          string   `json:"op"`
				KnowledgeID string   `json:"knowledge_id"`
				PageSlugs   []string `json:"page_slugs"`
				FolderIDs   []string `json:"folder_ids"`
			}
			if op.TenantID != tenant || op.TaskType != types.TypeWikiIngest || op.Op != "retract" ||
				op.ClaimedAt != nil || op.DedupKey == "" || json.Unmarshal(op.Payload, &payload) != nil ||
				payload.Op != op.Op || payload.KnowledgeID != op.DedupKey ||
				len(payload.PageSlugs) != 0 || len(payload.FolderIDs) != 0 {
				return errors.New("operation is not an unclaimed, target-free retraction")
			}
			var doc document
			// Metadata only: permission errors on a protected body must never
			// count as deletion. Require the persisted deletion marker itself.
			if err := tx.Table("knowledges").Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id,deleted_at").Where("id=? AND tenant_id=? AND knowledge_base_id=?",
				op.DedupKey, tenant, kb).Take(&doc).Error; err != nil {
				return err
			}
			if doc.DeletedAt == nil {
				return errors.New("retraction source is not confirmed deleted")
			}
			targets[doc.ID] = true
			documents = append(documents, doc)
		}
		var pages []struct {
			ID         string
			SourceRefs types.JSON
		}
		// Source restrictions may deliberately hide pages in ORM projections.
		// Absence must inspect ALL scoped reference metadata, never filtered
		// results. No title, summary, body, chunk or file is selected here.
		if err := tx.Raw(`SELECT id,source_refs FROM wiki_pages
 WHERE tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL ORDER BY id LIMIT ?`,
			tenant, kb, wikiMCPMaxPages+1).Scan(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return errors.New("wiki metadata bound exceeded")
		}
		for _, page := range pages {
			var refs []string
			if len(page.SourceRefs) > 0 && json.Unmarshal(page.SourceRefs, &refs) != nil {
				return errors.New("wiki reference metadata is unknown")
			}
			for _, ref := range refs {
				if ref == "" || targets[types.WikiSourceKnowledgeID(ref)] {
					return errors.New("wiki reference remains or cannot be verified")
				}
			}
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan = &WikiEmptyRetractionPlan{
			Mode: "retire_empty_retractions", TenantID: tenant, KnowledgeBaseID: kb,
			PendingOperations: len(pending), CurrentPages: len(pages),
			Snapshot: wikiDigest([]any{tenant, kb, pending, documents, pages, epoch}),
		}
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("empty retraction snapshot changed; preview again")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, op := range pending {
			payload, err := json.Marshal(map[string]any{
				"original_pending_id": op.ID, "original_scope": op.Scope,
				"payload_sha256": fmt.Sprintf("%x", sha256.Sum256(op.Payload)),
				"snapshot":       plan.Snapshot, "verification": "deleted_source_without_wiki_targets",
			})
			if err != nil {
				return err
			}
			if err := tx.Create(&types.TaskDeadLetter{
				TenantID: tenant, TaskType: "wiki:verified_empty_retraction",
				Scope: wikiRegenerationArchiveScope, ScopeID: kb, RelatedID: op.DedupKey,
				Payload: payload, FailCount: op.FailCount, FailedAt: time.Now().UTC(),
				LastError: "verified empty retraction; original operation retained; no content authority granted",
			}).Error; err != nil {
				return err
			}
			result := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND scope=? AND claimed_at IS NULL", op.ID, types.TaskScopeKnowledgeBase).
				Update("scope", wikiRegenerationArchiveScope)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("original retraction changed")
			}
		}
		plan.Applied = true
		return ctx.Err()
	})
	return plan, err
}
