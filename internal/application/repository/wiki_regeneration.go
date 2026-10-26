package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WikiRegenerationPlan contains no historical bodies or employee credentials.
type WikiRegenerationPlan struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Documents       int    `json:"documents"`
	HistoricalPages int    `json:"historical_pages"`
	Snapshot        string `json:"snapshot"`
	Applied         bool   `json:"applied"`
}

// PrepareWikiRegeneration previews or atomically isolates historical pages and
// queues regeneration from current verified inputs. Callers must hold wikilock
// with the same Redis configuration as the worker (or stop Lite workers).
// The expected snapshot is mandatory for apply; changed inputs reject the reset.
func PrepareWikiRegeneration(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiRegenerationPlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("tenant, knowledge base and apply snapshot are required")
	}
	var plan *WikiRegenerationPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND tenant_id=?", kb, tenant).First(&anchor).Error; err != nil {
			return err
		}
		if !anchor.IsWikiEnabled() {
			return errors.New("wiki is not enabled for this knowledge base")
		}
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		var pending int64
		if err := tx.Table("task_pending_ops").Where("scope='knowledge_base' AND scope_id=?", kb).
			Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return errors.New("knowledge base has pending operations; finish or cancel them before regeneration")
		}
		var docs []types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("knowledge_base_id=? AND tenant_id=?", kb, tenant).
			Order("id").Limit(2049).Find(&docs).Error; err != nil {
			return err
		}
		for _, doc := range docs {
			if doc.ParseStatus != types.ParseStatusCompleted || doc.PendingSubtasksCount != 0 {
				return errors.New("all documents must be completed without pending subtasks")
			}
		}
		// This validates source authority, input chunks, source epochs, active
		// tenants and supported bounds without reading ANY historical Wiki text.
		proof, err := wikiDependencies(tx, kb)
		if err != nil {
			return err
		}
		if proof.Tenant != tenant {
			return ErrWikiMCPUnverified
		}
		var pages []types.WikiPage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("knowledge_base_id=? AND tenant_id=?", kb, tenant).
			Order("id").Limit(1001).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > 1000 {
			return errors.New("regeneration page limit exceeded (1000)")
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan = &WikiRegenerationPlan{
			TenantID: tenant, KnowledgeBaseID: kb,
			Documents: len(docs), HistoricalPages: len(pages),
			Snapshot: wikiDigest([]any{anchor, proof.Sources, proof.Epochs, docs, pages, epoch}),
		}
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("regeneration snapshot changed; preview again")
		}
		batch := uuid.NewString()
		now := time.Now()
		for _, page := range pages {
			// Preserve old bodies and revision history, while releasing unique
			// slugs. Deleted pages cannot enter generation prompts or MCP reads.
			if err := tx.Model(&types.WikiPage{}).Where("id=?", page.ID).Updates(map[string]any{
				"slug":       "regeneration/" + batch + "/" + page.ID,
				"deleted_at": now, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		for _, doc := range docs {
			var attempt int
			if err := tx.Table("knowledge_processing_spans").Select("COALESCE(MAX(attempt),0)").
				Where("knowledge_id=?", doc.ID).Scan(&attempt).Error; err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{
				"op": "ingest", "knowledge_id": doc.ID, "attempt": attempt, "language": "zh-CN",
			})
			if err != nil {
				return err
			}
			res := tx.Model(&types.Knowledge{}).
				Where("id=? AND parse_status='completed' AND pending_subtasks_count=0", doc.ID).
				Updates(map[string]any{
					"parse_status": "finalizing", "pending_subtasks_count": 1,
					"error_message": "", "updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("document changed during regeneration: %s", doc.ID)
			}
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:ingest",
				Scope: "knowledge_base", ScopeID: kb, Op: "ingest", DedupKey: doc.ID,
				Payload: payload, EnqueuedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		// Recheck the entire source closure after preparation. No old page is
		// blessed and no input snapshot is allowed to drift at commit time.
		fresh, err := wikiDependencies(tx, kb)
		if err != nil {
			return err
		}
		if wikiDigest(fresh.Sources) != wikiDigest(proof.Sources) ||
			wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return errors.New("source inputs changed during regeneration")
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
