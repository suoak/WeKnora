package repository

import (
	"context"
	"crypto/sha256"
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
	TenantID         uint64 `json:"tenant_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id"`
	Documents        int    `json:"documents"`
	HistoricalPages  int    `json:"historical_pages"`
	Snapshot         string `json:"snapshot"`
	Applied          bool   `json:"applied"`
	ResumedPending   int    `json:"resumed_pending"`
	RetiredRetracts  int    `json:"retired_retracts"`
	RetiredFinalizes int    `json:"retired_finalizes"`
}

const wikiRegenerationArchiveScope = "wiki_regeneration_archive"

// WikiRegenerationRequiredMessage identifies a retained, indexed document whose Wiki inputs need regeneration.
const WikiRegenerationRequiredMessage = "Wiki post-processing requires verified source regeneration; " +
	"parsed indexes are retained"

// PrepareWikiRegeneration previews or atomically isolates historical pages and
// queues regeneration from current verified inputs. Callers must hold wikilock
// with the same Redis configuration as the worker (or stop Lite workers).
// The expected snapshot is mandatory for apply; changed inputs reject the reset.
func PrepareWikiRegeneration(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
	resumePending ...bool,
) (*WikiRegenerationPlan, error) {
	resume := len(resumePending) == 1 && resumePending[0]
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
		var pending []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope='knowledge_base' AND scope_id=?", kb).Order("id").Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) != 0 && !resume {
			return errors.New("knowledge base has pending operations; finish or cancel them before regeneration")
		}
		pendingByDocument := make(map[string]int, len(pending))
		var retracts []types.TaskPendingOp
		finalizes := 0
		for _, op := range pending {
			// A verified full rebuild supersedes unclaimed convergence intents as
			// well as ingest work. Archive their original payloads without reading
			// them into generation; never replace a live claim or unrelated lane.
			if op.TenantID != tenant || op.ClaimedAt != nil {
				return errors.New("pending Wiki operations are not safely resumable")
			}
			if op.TaskType == "wiki:finalize" &&
				(op.Op == "slug" || op.Op == "change" || op.Op == "folder_prune") {
				finalizes++
				continue
			}
			if op.TaskType != "wiki:ingest" ||
				(op.Op != "ingest" && op.Op != "retract") ||
				op.ClaimedAt != nil || op.DedupKey == "" {
				return errors.New("pending Wiki operations are not safely resumable")
			}
			if op.Op == "ingest" {
				pendingByDocument[op.DedupKey]++
			} else {
				retracts = append(retracts, op)
			}
		}
		for _, op := range retracts {
			var target struct{ DeletedAt *time.Time }
			err := tx.Table("knowledges").Select("deleted_at").
				Where("id=? AND tenant_id=? AND knowledge_base_id=?", op.DedupKey, tenant, kb).Take(&target).Error
			if err != nil || (target.DeletedAt == nil && pendingByDocument[op.DedupKey] != 1) {
				return errors.New("wiki retraction has no verified deleted or replacement document")
			}
		}
		var docs []types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("knowledge_base_id=? AND tenant_id=?", kb, tenant).
			Order("id").Limit(2049).Find(&docs).Error; err != nil {
			return err
		}
		for _, doc := range docs {
			completed := doc.ParseStatus == types.ParseStatusCompleted && doc.PendingSubtasksCount == 0 &&
				pendingByDocument[doc.ID] == 0
			waitingOnlyForWiki := resume && doc.ParseStatus == types.ParseStatusFinalizing &&
				doc.PendingSubtasksCount == 1 && doc.SummaryStatus == types.SummaryStatusCompleted &&
				(doc.ErrorMessage == "" || doc.ErrorMessage == WikiRegenerationRequiredMessage) &&
				pendingByDocument[doc.ID] == 1
			if !completed && !waitingOnlyForWiki {
				return errors.New("all documents must be completed without pending subtasks")
			}
			delete(pendingByDocument, doc.ID)
		}
		if len(pendingByDocument) != 0 {
			return errors.New("pending Wiki operation has no current document")
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
			Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return errors.New("regeneration page limit exceeded (20000)")
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan = &WikiRegenerationPlan{
			TenantID: tenant, KnowledgeBaseID: kb,
			Documents: len(docs), HistoricalPages: len(pages),
			ResumedPending: len(pending) - len(retracts) - finalizes, RetiredRetracts: len(retracts),
			RetiredFinalizes: finalizes,
			Snapshot: wikiDigest([]any{
				anchor, proof.Sources, proof.Epochs, docs, pages, epoch, resume, pending,
			}),
		}
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("regeneration snapshot changed; preview again")
		}
		type documentEpoch struct {
			KnowledgeID string
			Generation  int64
			Incarnation string
		}
		epochs := make(map[string]documentEpoch, len(docs))
		for _, doc := range docs {
			var e documentEpoch
			if err := tx.Table("document_access_epochs").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("knowledge_id=?", doc.ID).Take(&e).Error; err != nil {
				return err
			}
			epochs[doc.ID] = e
		}
		batch := uuid.NewString()
		now := time.Now()
		for _, op := range pending {
			// Keep the original row and payload in a non-consumable SQL scope.
			// The public failure ledger receives only identity/hash evidence,
			// never unknown historical queue text.
			payload, err := json.Marshal(map[string]any{
				"original_pending_id": op.ID, "original_scope": op.Scope,
				"original_op": op.Op, "payload_sha256": fmt.Sprintf("%x", sha256.Sum256(op.Payload)), "batch": batch,
			})
			if err != nil {
				return err
			}
			if err := tx.Create(&types.TaskDeadLetter{
				TenantID: tenant, TaskType: "wiki:regeneration_archive",
				Scope: wikiRegenerationArchiveScope, ScopeID: kb,
				RelatedID: op.DedupKey, Payload: payload, FailCount: op.FailCount,
				LastError: "superseded by verified Wiki regeneration " + batch, FailedAt: now,
			}).Error; err != nil {
				return err
			}
			res := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND scope='knowledge_base' AND claimed_at IS NULL", op.ID).
				Update("scope", wikiRegenerationArchiveScope)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errors.New("pending Wiki claim changed during regeneration")
			}
		}
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
		slotEpochAdvance := make(map[string]int64, len(docs))
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
			// Published Feishu rows belong to the fenced source publication
			// protocol. A Wiki-only rebuild must not reopen their parse lifecycle.
			// These completed inputs have no slot to release; durable Wiki ops
			// provide progress and the normal finalizer is already a no-op at zero.
			independentWiki := legacySourceKind(doc.Channel) &&
				doc.ParseStatus == types.ParseStatusCompleted && doc.PendingSubtasksCount == 0
			if !independentWiki {
				res := tx.Model(&types.Knowledge{}).
					Where("id=? AND parse_status=? AND pending_subtasks_count=?",
						doc.ID, doc.ParseStatus, doc.PendingSubtasksCount).
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
				slotEpochAdvance[doc.ID] = 2
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
		if wikiDigest(fresh.Sources) != wikiDigest(proof.Sources) {
			return errors.New("source inputs changed during regeneration")
		}
		// A seeded slot advances the epoch twice (OLD and NEW trigger branches).
		// Independent completed Feishu inputs must have no epoch change at all.
		for id, before := range epochs {
			var after documentEpoch
			if err := tx.Table("document_access_epochs").Where("knowledge_id=?", id).Take(&after).Error; err != nil {
				return err
			}
			if after.Incarnation != before.Incarnation || after.Generation != before.Generation+slotEpochAdvance[id] {
				return errors.New("unexpected document epoch change during regeneration")
			}
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
