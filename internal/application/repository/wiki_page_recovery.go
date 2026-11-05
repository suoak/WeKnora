package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WikiPageRecoveryPlan contains identifiers and counts, never historical bodies.
// Valid pages and every existing durable operation retain their original identity.
type WikiPageRecoveryPlan struct {
	TenantID        uint64   `json:"tenant_id"`
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	RetainedPages   int      `json:"retained_pages"`
	IsolatedPages   []string `json:"isolated_pages"`
	QueueDocuments  []string `json:"queue_documents"`
	PendingOps      int      `json:"pending_operations"`
	RetiredRetracts []int64  `json:"retired_retractions"`
	Snapshot        string   `json:"snapshot"`
	Applied         bool     `json:"applied"`
}

// PrepareWikiPageRecovery is the selective form of the existing operator
// regeneration protocol. The caller holds wikilock, preview is read-only and
// apply is fenced by the exact source/page/operation snapshot. It never seals
// historical text or marks business work successful.
func PrepareWikiPageRecovery(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiPageRecoveryPlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("tenant, knowledge base and apply snapshot are required")
	}
	var plan *WikiPageRecoveryPlan
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
		var pending []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope='knowledge_base' AND scope_id=?", kb).Order("id").Find(&pending).Error; err != nil {
			return err
		}
		existing := map[string]bool{}
		for _, op := range pending {
			if op.TenantID != tenant || op.ClaimedAt != nil ||
				(op.TaskType != "wiki:ingest" && op.TaskType != "wiki:finalize") {
				return errors.New("pending Wiki operations are not safely resumable")
			}
			if op.TaskType == "wiki:ingest" && op.Op == "ingest" {
				existing[op.DedupKey] = true
			}
		}
		proof, err := wikiDependencies(tx, kb)
		if err != nil {
			return err
		}
		if proof.Tenant != tenant {
			return ErrWikiMCPUnverified
		}
		// A retraction's old body cannot become a new model input. After the
		// invalid pages are isolated, all retained pages already prove current
		// versions. Only confirmed deletion or a current replacement ingest
		// can supersede the original retraction, which is preserved in audit.
		deletedEvidence := map[string]string{}
		var retracts []types.TaskPendingOp
		for _, op := range pending {
			if op.TaskType != "wiki:ingest" || op.Op != "retract" {
				continue
			}
			var identity struct {
				Op          string `json:"op"`
				KnowledgeID string `json:"knowledge_id"`
			}
			if json.Unmarshal(op.Payload, &identity) != nil {
				return errors.New("retraction identity is unknown")
			}
			if identity.Op != "retract" || identity.KnowledgeID != op.DedupKey || op.DedupKey == "" {
				return errors.New("retraction identity is inconsistent")
			}
			var document struct{ DeletedAt *time.Time }
			err := tx.Table("knowledges").Select("deleted_at").Where(
				"id=? AND tenant_id=? AND knowledge_base_id=?", op.DedupKey, tenant, kb).Take(&document).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				evidence, err := deletedWikiInputEvidence(tx, tenant, kb, op.DedupKey)
				if err != nil {
					return err
				}
				deletedEvidence[op.DedupKey] = evidence
			} else if err != nil {
				return err
			} else if document.DeletedAt != nil {
				deletedEvidence[op.DedupKey] = wikiDigest(document)
			} else if !existing[op.DedupKey] || proof.Sources[op.DedupKey] == "" {
				return errors.New("retraction has no confirmed deletion or verified replacement")
			}
			retracts = append(retracts, op)
		}
		var pages []types.WikiPage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"knowledge_base_id=? AND tenant_id=?", kb, tenant).
			Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return errors.New("recovery page limit exceeded (20000)")
		}
		var seals []WikiMCPProvenance
		if err := tx.Where("knowledge_base_id=? AND tenant_id=?", kb, tenant).
			Order("page_id").Find(&seals).Error; err != nil {
			return err
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan = &WikiPageRecoveryPlan{
			TenantID: tenant, KnowledgeBaseID: kb, PendingOps: len(pending),
			IsolatedPages: []string{}, QueueDocuments: []string{}, RetiredRetracts: []int64{},
		}
		for _, op := range retracts {
			plan.RetiredRetracts = append(plan.RetiredRetracts, op.ID)
		}
		queue := map[string]bool{}
		for i := range pages {
			page := &pages[i]
			err := verifyWikiProof(tx, page, proof, false, nil)
			if err == nil {
				plan.RetainedPages++
				continue
			}
			if !errors.Is(err, ErrWikiMCPUnverified) {
				return err
			}
			plan.IsolatedPages = append(plan.IsolatedPages, page.ID)
			// Historical references are scheduling hints only. Only documents
			// independently proved by the current closure may enter generation.
			for _, ref := range page.SourceRefs {
				id := types.WikiSourceKnowledgeID(ref)
				if proof.Sources[id] != "" && !proof.EmptySources[id] && !existing[id] {
					queue[id] = true
				}
			}
		}
		for id := range queue {
			plan.QueueDocuments = append(plan.QueueDocuments, id)
		}
		sort.Strings(plan.QueueDocuments)
		plan.Snapshot = wikiDigest([]any{anchor, proof.Sources, proof.Epochs, pages, seals, pending, epoch, deletedEvidence})
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("recovery snapshot changed")
		}
		now := time.Now().UTC()
		for _, id := range plan.IsolatedPages {
			result := tx.Model(&types.WikiPage{}).Where("id=? AND tenant_id=? AND knowledge_base_id=?", id, tenant, kb).
				Updates(map[string]any{
					"slug": "recovery/" + plan.Snapshot + "/" + id, "deleted_at": now, "updated_at": now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("recovery page changed")
			}
		}
		for _, id := range plan.QueueDocuments {
			var attempt int
			if err := tx.Table("knowledge_processing_spans").Select("COALESCE(MAX(attempt),0)").
				Where("knowledge_id=?", id).Scan(&attempt).Error; err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{
				"op": "ingest", "knowledge_id": id, "attempt": attempt, "language": "zh-CN",
			})
			if err != nil {
				return err
			}
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: kb,
				Op: "ingest", DedupKey: id, Payload: payload, EnqueuedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		for _, op := range retracts {
			result := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND tenant_id=? AND scope='knowledge_base' AND claimed_at IS NULL", op.ID, tenant).
				Update("scope", wikiRegenerationArchiveScope)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("retraction claim changed during recovery")
			}
		}
		fresh, err := wikiDependencies(tx, kb)
		if err != nil {
			return err
		}
		if wikiDigest(fresh.Sources) != wikiDigest(proof.Sources) ||
			wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return errors.New("recovery source inputs changed")
		}
		plan.Applied = true
		payload, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		if err := tx.Create(&types.TaskPendingOp{
			TenantID: tenant, TaskType: "wiki:page_recovery", Scope: "wiki_page_recovery_audit", ScopeID: kb,
			Op: "applied", DedupKey: plan.Snapshot, Payload: payload, EnqueuedAt: now,
		}).Error; err != nil {
			return err
		}
		return WriteWikiProcessingBlock(ctx, NewWikiPageRepository(tx), tenant, kb, "")
	})
	return plan, err
}
