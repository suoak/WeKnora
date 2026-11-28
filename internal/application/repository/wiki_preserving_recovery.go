package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const wikiPreservingScope = "wiki_preserving_recovery"

// Snapshot timestamps bind instants, not the scanning process's time zone.
// Keep every field and timestamp precision; never reuse a changed historical row.
func wikiPreservingPageDigest(page *types.WikiPage) string {
	cloned := *page
	cloned.CreatedAt, cloned.UpdatedAt = cloned.CreatedAt.UTC(), cloned.UpdatedAt.UTC()
	if cloned.DeletedAt.Valid {
		cloned.DeletedAt.Time = cloned.DeletedAt.Time.UTC()
	}
	return wikiDigest(&cloned)
}

func wikiPreservingPageUnchanged(page *types.WikiPage, expected string) bool {
	// Old recovery markers contain the original process representation. Accept
	// that exact digest as well as UTC; neither path ignores a field or instant.
	return expected != "" && (wikiPreservingPageDigest(page) == expected || wikiDigest(page) == expected)
}

// WikiPreservingPlan describes a snapshot-bound, non-destructive regeneration.
type WikiPreservingPlan struct {
	TenantID          uint64            `json:"tenant_id"`
	KnowledgeBaseID   string            `json:"knowledge_base_id"`
	Documents         []string          `json:"documents"`
	RetiredRetracts   []int64           `json:"retired_retracts,omitempty"`
	HistoricalPages   int               `json:"historical_pages"`
	UnverifiedPages   int               `json:"unverified_pages"`
	PendingOperations int               `json:"pending_operations"`
	SourceSHA256      string            `json:"source_sha256"`
	Snapshot          string            `json:"snapshot"`
	Applied           bool              `json:"applied"`
	OriginalPages     map[string]string `json:"original_pages"`
}

// WikiPreservingRecoveryActive checks only the operator-issued recovery marker.
// It is not source read authority; generation still proves every current input.
func WikiPreservingRecoveryActive(ctx context.Context, repo interfaces.WikiPageRepository, kb string) (bool, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return false, ErrWikiMCPUnverified
	}
	var rows []types.TaskPendingOp
	if err := r.db.WithContext(ctx).Where("scope=? AND scope_id=? AND task_type=? AND op='active'",
		wikiPreservingScope, kb, "wiki:preserve_recovery").Limit(2).Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	if len(rows) != 1 {
		return false, ErrWikiMCPUnverified
	}
	var plan WikiPreservingPlan
	if json.Unmarshal(rows[0].Payload, &plan) != nil || !plan.Applied || plan.TenantID != rows[0].TenantID ||
		plan.KnowledgeBaseID != kb || plan.Snapshot != rows[0].DedupKey || len(plan.SourceSHA256) != 64 {
		return false, ErrWikiMCPUnverified
	}
	return true, nil
}

// PrepareWikiPreservingRecovery queues current verified source documents and
// authorizes a scoped worker mode. Preview and apply never alter old pages,
// historical proofs, document lifecycles or source cursors. Superseded
// replacement retractions are retained intact in an audit namespace.
// The caller holds the same per-KB lock as generation.
func PrepareWikiPreservingRecovery(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiPreservingPlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("tenant, knowledge base and apply snapshot are required")
	}
	var plan *WikiPreservingPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", kb, tenant).
			First(&anchor).Error; err != nil {
			return err
		}
		if !anchor.IsWikiEnabled() {
			return errors.New("wiki is not enabled for this knowledge base")
		}
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		proof, err := wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		if proof.Tenant != tenant || len(proof.Sources) == 0 || len(proof.Sources) > 2048 {
			return ErrWikiMCPUnverified
		}
		var markerCount int64
		if err := tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?", wikiPreservingScope, kb).
			Count(&markerCount).Error; err != nil {
			return err
		}
		if markerCount != 0 {
			return errors.New("preserving recovery already exists; inspect its progress")
		}
		var pending []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope='knowledge_base' AND scope_id=?", kb).
			Order("id").Limit(4097).Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) > 4096 {
			return errors.New("pending operation bound exceeded")
		}
		existing := map[string]bool{}
		existingPayload := map[string]json.RawMessage{}
		var retracts []types.TaskPendingOp
		for _, op := range pending {
			if op.TenantID != tenant || op.ClaimedAt != nil || op.TaskType != "wiki:ingest" ||
				(op.Op != "ingest" && op.Op != "retract") || op.Op == "ingest" && proof.Sources[op.DedupKey] == "" {
				return errors.New("pending Wiki operations are not safely resumable")
			}
			var identity struct {
				Op          string `json:"op"`
				KnowledgeID string `json:"knowledge_id"`
			}
			if json.Unmarshal(op.Payload, &identity) != nil || identity.Op != op.Op ||
				identity.KnowledgeID != op.DedupKey {
				return errors.New("pending Wiki operation identity is inconsistent")
			}
			if op.Op == "retract" {
				retracts = append(retracts, op)
				continue
			}
			// The durable queue permits repeated keys. Preserve identical
			// deliveries for normal worker consumption; conflicting versions
			// still require an operator decision. Every original row remains
			// part of the preview/apply snapshot, including retry counters.
			if existing[op.DedupKey] && !bytes.Equal(existingPayload[op.DedupKey], op.Payload) {
				return errors.New("pending Wiki replacement is ambiguous")
			}
			existing[op.DedupKey] = true
			existingPayload[op.DedupKey] = op.Payload
		}
		deletedEvidence := make(map[string]string)
		for _, op := range retracts {
			if existing[op.DedupKey] && !proof.EmptySources[op.DedupKey] {
				continue
			}
			evidence, err := wikiPreservingDeletedEvidence(tx, tenant, kb, op.DedupKey)
			if err != nil {
				return err
			}
			deletedEvidence[op.DedupKey] = evidence
		}
		var pages []*types.WikiPage
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preserving_plan").
			Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).
			Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return errors.New("recovery page limit exceeded")
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan = &WikiPreservingPlan{
			TenantID: tenant, KnowledgeBaseID: kb, HistoricalPages: len(pages),
			OriginalPages:     map[string]string{},
			PendingOperations: len(pending), SourceSHA256: proof.DependencySHA256, Documents: []string{},
		}
		for _, op := range retracts {
			plan.RetiredRetracts = append(plan.RetiredRetracts, op.ID)
		}
		for id := range proof.Sources {
			if !proof.EmptySources[id] {
				plan.Documents = append(plan.Documents, id)
			}
		}
		sort.Strings(plan.Documents)
		for _, page := range pages {
			plan.OriginalPages[page.ID] = wikiPreservingPageDigest(page)
			if err := verifyWikiProof(tx, page, proof, false, nil); errors.Is(err, ErrWikiMCPUnverified) {
				plan.UnverifiedPages++
			} else if err != nil {
				return err
			}
		}
		plan.Snapshot = wikiDigest([]any{anchor, proof.Sources, proof.Epochs, pages, pending, epoch, deletedEvidence})
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("recovery snapshot changed")
		}
		for _, id := range plan.Documents {
			if existing[id] {
				continue
			}
			var attempt int
			if err := tx.Table("knowledge_processing_spans").Select("COALESCE(MAX(attempt),0)").
				Where("knowledge_id=?", id).Scan(&attempt).Error; err != nil {
				return err
			}
			payload, _ := json.Marshal(
				map[string]any{"op": "ingest", "knowledge_id": id, "attempt": attempt, "language": "zh-CN"},
			)
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:ingest", Scope: "knowledge_base",
				ScopeID: kb, Op: "ingest", DedupKey: id, Payload: payload, EnqueuedAt: time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
		}
		for _, op := range retracts {
			// Preserve the original ID, payload, counter and enqueue timestamp
			// in an audit namespace, atomically with the replacement marker.
			updated := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND scope='knowledge_base' AND scope_id=? AND tenant_id=? AND claimed_at IS NULL",
					op.ID, kb, tenant).
				Update("scope", "wiki_preserve_retract_archive")
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrWikiMCPInputsChanged
			}
		}
		plan.Applied = true
		fresh, err := wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		if fresh.DependencySHA256 != proof.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return ErrWikiMCPInputsChanged
		}
		payload, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		if err := tx.Create(&types.TaskPendingOp{
			TenantID: tenant, TaskType: "wiki:preserve_recovery", Scope: wikiPreservingScope,
			ScopeID: kb, Op: "active", DedupKey: plan.Snapshot, Payload: payload, EnqueuedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		return WriteWikiProcessingBlock(ctx, NewWikiPageRepository(tx), tenant, kb, "")
	})
	return plan, err
}

// WikiPreservingFinishPlan describes archival after every document has a
// current verified summary and original durable generation work has finished.
type WikiPreservingFinishPlan struct {
	TenantID        uint64   `json:"tenant_id"`
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	RetainedPages   int      `json:"retained_pages"`
	RetiredPages    []string `json:"retired_pages"`
	Snapshot        string   `json:"snapshot"`
	Applied         bool     `json:"applied"`
}

// FinishWikiPreservingRecovery never hides old pages until their replacement
// source coverage is complete. Changed historical rows require separate review.
func FinishWikiPreservingRecovery(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiPreservingFinishPlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("exact recovery snapshot required")
	}
	var plan *WikiPreservingFinishPlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", kb, tenant).
			First(&anchor).Error; err != nil {
			return err
		}
		var markers []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope=? AND scope_id=? AND tenant_id=? AND op='active'", wikiPreservingScope, kb, tenant).
			Limit(2).Find(&markers).Error; err != nil {
			return err
		}
		if len(markers) != 1 {
			return errors.New("one active preserving recovery required")
		}
		var original WikiPreservingPlan
		if json.Unmarshal(markers[0].Payload, &original) != nil || !original.Applied ||
			original.KnowledgeBaseID != kb ||
			original.TenantID != tenant ||
			len(original.OriginalPages) != original.HistoricalPages {
			return ErrWikiMCPUnverified
		}
		var pending int64
		if err := tx.Model(&types.TaskPendingOp{}).
			Where("scope='knowledge_base' AND scope_id=? AND tenant_id=? AND task_type IN ?",
				kb, tenant, []string{"wiki:ingest", "wiki:finalize"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return errors.New("wiki generation is still pending")
		}
		proof, err := wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		if proof.Tenant != tenant {
			return ErrWikiMCPUnverified
		}
		var pages []*types.WikiPage
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preserving_finish").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).
			Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return ErrWikiMCPUnverified
		}
		plan = &WikiPreservingFinishPlan{TenantID: tenant, KnowledgeBaseID: kb, RetiredPages: []string{}}
		covered := map[string]bool{}
		for _, page := range pages {
			err := verifyWikiProof(tx, page, proof, false, nil)
			if err == nil {
				plan.RetainedPages++
				if page.Status == types.WikiPageStatusPublished && page.PageType == types.WikiPageTypeSummary {
					for _, ref := range page.SourceRefs {
						covered[types.WikiSourceKnowledgeID(ref)] = true
					}
				}
				continue
			}
			if !errors.Is(err, ErrWikiMCPUnverified) {
				return err
			}
			if wikiPreservingPageUnchanged(page, original.OriginalPages[page.ID]) {
				plan.RetiredPages = append(plan.RetiredPages, page.ID)
			} else {
				return errors.New("historical Wiki page changed after recovery; preserve for review")
			}
		}
		for id := range proof.Sources {
			if !proof.EmptySources[id] && !covered[id] {
				return errors.New("verified summary coverage is incomplete")
			}
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		plan.Snapshot = wikiDigest([]any{anchor, markers[0], proof.Sources, proof.Epochs, pages, epoch})
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("recovery snapshot changed")
		}
		for _, id := range plan.RetiredPages {
			result := tx.Model(&types.WikiPage{}).Where("id=? AND tenant_id=? AND knowledge_base_id=?", id, tenant, kb).
				Updates(map[string]any{
					"slug":       "superseded/" + original.Snapshot + "/" + id,
					"deleted_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWikiPageConflict
			}
		}
		plan.Applied = true
		fresh, err := wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		if fresh.DependencySHA256 != proof.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return ErrWikiMCPInputsChanged
		}
		payload, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		result := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='active'", markers[0].ID).
			Updates(map[string]any{"op": "completed", "payload": json.RawMessage(payload)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWikiPageConflict
		}
		return WriteWikiProcessingBlock(ctx, NewWikiPageRepository(tx), tenant, kb, "")
	})
	return plan, err
}

// Absence alone never authorizes retiring a retraction. Existing rows must
// carry a scoped deletion marker; hard-deleted rows need retained epoch/chunk
// evidence from the same tenant and KB. Locks remain held through commit.
func wikiPreservingDeletedEvidence(tx *gorm.DB, tenant uint64, kb, id string) (string, error) {
	var row struct {
		ID        string
		DeletedAt *time.Time
	}
	err := tx.Table("knowledges").Select("id,deleted_at").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id=? AND tenant_id=? AND knowledge_base_id=?", id, tenant, kb).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return deletedWikiInputEvidence(tx, tenant, kb, id)
	}
	if err != nil {
		return "", err
	}
	if row.DeletedAt == nil {
		return "", errors.New("retraction has no verified deletion or replacement ingest")
	}
	return wikiDigest(row), nil
}
