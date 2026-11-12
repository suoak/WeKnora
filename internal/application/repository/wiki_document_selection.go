package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrWikiNoEligibleDocuments indicates that no eligible document can supply Wiki input.
var ErrWikiNoEligibleDocuments = errors.New("wiki has no currently verified documents")

const wikiDocumentSkipScope = "wiki_document_skip"

// A new source publication can enqueue the same identity while an old row is
// parked. Keep the historical row parked until the live operation settles;
// never overwrite its payload or fail the whole KB's restoration transaction.
const wikiRestoreNoLiveDuplicate = `NOT EXISTS(SELECT 1 FROM task_pending_ops live
WHERE live.tenant_id=task_pending_ops.tenant_id AND live.scope='knowledge_base'
AND live.scope_id=task_pending_ops.scope_id AND live.task_type=task_pending_ops.task_type
AND live.op=task_pending_ops.op AND live.dedup_key=task_pending_ops.dedup_key)`

// WikiDocumentSelection contains identifiers and reasons, never document text.
// It is created internally; callers cannot supply a replacement content proof.
type WikiDocumentSelection struct {
	Eligible      map[string]bool   `json:"eligible"`
	Skipped       map[string]string `json:"skipped"`
	IsolatedPages int               `json:"isolated_pages"`
}

type wikiDocumentSelectionKey struct{}

// WikiSelectedDocument checks whether the document belongs to the eligible Wiki selection.
func WikiSelectedDocument(ctx context.Context, id string) (bool, bool) {
	s, ok := ctx.Value(wikiDocumentSelectionKey{}).(*WikiDocumentSelection)
	if !ok {
		return false, false
	}
	return s.Eligible[id], true
}

// BeginWikiDocumentGeneration replaces the all-or-nothing worker preflight.
// Each candidate is independently proved. Unusable historical pages are moved
// out of the live slug namespace, retaining their original body for audit.
// Their independently verified source IDs are scheduled for regeneration.
func BeginWikiDocumentGeneration(
	ctx context.Context,
	repo interfaces.WikiPageRepository,
	kb string,
) (context.Context, *WikiDocumentSelection, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return ctx, nil, ErrWikiMCPUnverified
	}
	selection := &WikiDocumentSelection{Eligible: map[string]bool{}, Skipped: map[string]string{}}
	var proof *wikiGenerationProof
	err := r.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, kb)).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", kb).First(&anchor).Error; err != nil {
			return err
		}
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		var live struct{ TenantID uint64 }
		if err := tx.Table("knowledge_bases kb").Select("kb.tenant_id").
			Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND "+
				"t.status='active'").Where("kb.id=? AND kb.deleted_at IS NULL AND kb.type='document' AND "+
			"kb.is_temporary=false", kb).Take(&live).Error; err != nil {
			return err
		}
		var err error
		selection, proof, err = selectWikiDocumentInputs(tx, kb, anchor.TenantID)
		if err != nil {
			return err
		}
		for id := range selection.Eligible {
			if err := writeWikiDocumentSkip(tx, anchor.TenantID, kb, id, ""); err != nil {
				return err
			}
		}
		for id, reason := range selection.Skipped {
			if err := writeWikiDocumentSkip(tx, anchor.TenantID, kb, id, reason); err != nil {
				return err
			}
		}
		var pages []*types.WikiPage
		// Internal inspection only: no historical body is returned to the
		// caller. Each page is proved below or moved out of the live namespace.
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS isolation_inputs").
			Where("knowledge_base_id=? AND tenant_id=?", kb, anchor.TenantID).Order("id").
			Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return ErrWikiMCPUnverified
		}
		now := time.Now().UTC()
		for _, page := range pages {
			err := verifyWikiProof(tx, page, proof, false, nil)
			if err == nil {
				continue
			}
			if !errors.Is(err, ErrWikiMCPUnverified) {
				return err
			}
			// Preserve old content and proof. Changing the slug permits a clean
			// replacement without ever presenting unverified old text to a model.
			if err := tx.Model(&types.WikiPage{}).Where("id=? AND tenant_id=? AND knowledge_base_id=?",
				page.ID, anchor.TenantID, kb).Updates(map[string]any{"slug": "isolated/" + uuid.
				NewString() + "/" + page.ID, "deleted_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			selection.IsolatedPages++
			for _, ref := range page.SourceRefs {
				id := types.WikiSourceKnowledgeID(ref)
				if !selection.Eligible[id] {
					continue
				}
				var existing int64
				if err := tx.Model(&types.TaskPendingOp{}).
					Where("tenant_id=? AND scope='knowledge_base' AND scope_id=? AND "+
						"task_type='wiki:ingest' AND dedup_key=?", anchor.TenantID, kb, id).Count(&existing).
					Error; err != nil {
					return err
				}
				if existing != 0 {
					continue
				}
				var attempt int
				if err := tx.Table("knowledge_processing_spans").Select("COALESCE(MAX(attempt),0)").
					Where("knowledge_id=?", id).Scan(&attempt).Error; err != nil {
					return err
				}
				raw, _ := json.Marshal(
					map[string]any{"op": "ingest", "knowledge_id": id, "attempt": attempt, "language": "zh-CN"},
				)
				if err := tx.Create(&types.TaskPendingOp{
					TenantID: anchor.TenantID,
					TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: kb, Op: "ingest",
					DedupKey: id, Payload: raw, EnqueuedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		}
		// Only the same original operations are restored when their document
		// becomes eligible again. Payloads, attempts and retry counts survive.
		for id := range selection.Eligible {
			if err := tx.Model(&types.TaskPendingOp{}).
				Where("tenant_id=? AND scope='wiki_skipped_ops' AND scope_id=? AND "+
					"task_type='wiki:ingest' AND op='ingest' AND dedup_key=? AND "+
					"claimed_at IS NULL", anchor.TenantID, kb, id).Where(wikiRestoreNoLiveDuplicate).
				Update("scope", "knowledge_base").Error; err != nil {
				return err
			}
		}
		if len(selection.Eligible) > 0 {
			if err := tx.Model(&types.TaskPendingOp{}).
				Where("tenant_id=? AND scope='wiki_waiting_finalize' AND scope_id=? AND "+
					"task_type='wiki:finalize' AND claimed_at IS NULL", anchor.TenantID, kb).
				Where(wikiRestoreNoLiveDuplicate).Update("scope", "knowledge_base").Error; err != nil {
				return err
			}
		}
		epoch, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		proof.WikiGeneration, proof.WikiIncarnation = epoch.Generation, epoch.Incarnation
		return nil
	})
	if err != nil {
		return ctx, nil, err
	}
	ctx = context.WithValue(ctx, wikiDocumentSelectionKey{}, selection)
	if len(selection.Eligible) == 0 {
		return ctx, selection, ErrWikiNoEligibleDocuments
	}
	ctx = context.WithValue(ctx, wikiGenerationProofKey{}, proof)
	ctx = types.WithWikiVerifiedReadGuard(
		ctx,
		kb,
		func(db *gorm.DB, knowledge, target string, page *types.WikiPage) error {
			db = db.Session(&gorm.Session{NewDB: true})
			if target != kb || (knowledge != "" && !selection.Eligible[knowledge]) {
				return ErrWikiMCPUnverified
			}
			ids := selection.Eligible
			if knowledge != "" {
				ids = map[string]bool{knowledge: true}
			}
			fresh, err := wikiDependenciesSelected(db, kb, nil, ids, proof)
			if err != nil {
				return err
			}
			for id := range ids {
				if fresh.Sources[id] != proof.Sources[id] || fresh.Epochs[id] != proof.Epochs[id] {
					return ErrWikiMCPInputsChanged
				}
			}
			if page != nil {
				return verifyWikiProof(db, page, fresh, false, nil)
			}
			return nil
		},
	)
	return ctx, selection, nil
}

// ParkWikiFinalizeWithoutInputs retains finalize work until eligible inputs are available.
func ParkWikiFinalizeWithoutInputs(
	ctx context.Context,
	repo interfaces.WikiPageRepository,
	kb string,
	ids []int64,
) error {
	r, ok := repo.(*wikiPageRepository)
	s, selected := ctx.Value(wikiDocumentSelectionKey{}).(*WikiDocumentSelection)
	if !ok || !selected || len(s.Eligible) > 0 {
		return ErrWikiMCPUnverified
	}
	return r.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, kb)).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", kb).First(&anchor).Error; err != nil {
			return err
		}
		result := tx.Model(&types.TaskPendingOp{}).
			Where("id IN ? AND tenant_id=? AND scope='knowledge_base' AND scope_id=? "+
				"AND task_type='wiki:finalize' AND claimed_at IS NULL", ids, anchor.TenantID, kb).
			Update("scope", "wiki_waiting_finalize")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return errors.New("wiki finalize ownership changed")
		}
		return nil
	})
}

// ArchiveWikiSkippedOperations removes only this worker's excluded operations
// from the executable lane. Their full original rows remain available for retry.
// Retractions carry historical text; page isolation already removed that text
// from the live closure, so retain the original intent outside the model lane.
func ArchiveWikiSkippedOperations(
	ctx context.Context,
	repo interfaces.WikiPageRepository,
	kb string,
	ids []int64,
	claims map[int64]time.Time,
) ([]int64, error) {
	r, ok := repo.(*wikiPageRepository)
	selection, selected := ctx.Value(wikiDocumentSelectionKey{}).(*WikiDocumentSelection)
	if !ok || !selected {
		return nil, ErrWikiMCPUnverified
	}
	var archived []int64
	err := r.db.WithContext(types.WithLegacyKBAdminMetadata(ctx, kb)).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", kb).First(&anchor).Error; err != nil {
			return err
		}
		for _, id := range ids {
			var op types.TaskPendingOp
			if err := tx.Where("id=? AND tenant_id=? AND scope='knowledge_base' AND scope_id=? AND "+
				"task_type='wiki:ingest'", id, anchor.TenantID, kb).First(&op).Error; err != nil {
				return err
			}
			var identity struct {
				Op          string `json:"op"`
				KnowledgeID string `json:"knowledge_id"`
			}
			validIdentity := json.Unmarshal(op.Payload, &identity) == nil && identity.Op == op.Op &&
				identity.KnowledgeID == op.DedupKey &&
				op.DedupKey != ""
			if op.Op == "ingest" && validIdentity && selection.Eligible[op.DedupKey] {
				continue
			}
			q := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND scope='knowledge_base' AND scope_id=? AND tenant_id=?", id, kb, anchor.TenantID)
			if stamp, claimed := claims[id]; claimed {
				q = q.Where("claimed_at=?", stamp)
			} else {
				q = q.Where("claimed_at IS NULL")
			}
			scope := "wiki_skipped_ops"
			if op.Op != "ingest" || !validIdentity {
				scope = "wiki_regeneration_archive"
			}
			result := q.Updates(map[string]any{"scope": scope, "claimed_at": nil})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("wiki skipped operation ownership changed")
			}
			archived = append(archived, id)
		}
		return nil
	})
	return archived, err
}

func writeWikiDocumentSkip(tx *gorm.DB, tenant uint64, kb, id, reason string) error {
	var row types.TaskPendingOp
	err := tx.Where("tenant_id=? AND scope=? AND scope_id=? AND "+
		"task_type='wiki:document_skip' AND dedup_key=?", tenant, wikiDocumentSkipScope, kb, id).
		First(&row).
		Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if reason == "" && errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	state := "skipped"
	if reason == "" {
		state = "resolved"
	}
	raw, _ := json.Marshal(
		map[string]any{
			"knowledge_id": id,
			"reason":       reason,
			"checked_at":   time.Now().UTC(),
			"retry_after":  time.Now().UTC().Add(30 * time.Minute),
		},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(
			&types.TaskPendingOp{
				TenantID:   tenant,
				Scope:      wikiDocumentSkipScope,
				ScopeID:    kb,
				TaskType:   "wiki:document_skip",
				DedupKey:   id,
				Op:         state,
				Payload:    raw,
				EnqueuedAt: time.Now().UTC(),
			},
		).Error
	}
	return tx.Model(&row).
		Updates(map[string]any{"op": state, "payload": raw, "enqueued_at": time.Now().UTC()}).
		Error
}

// WikiSkipRecheckTarget identifies skipped work for a scoped eligibility recheck.
type WikiSkipRecheckTarget struct {
	TenantID        uint64
	KnowledgeBaseID string
}

// RecheckWikiSkippedOperations checks a bounded set of local publication facts.
// It performs no remote requests or model work. Only an already valid current
// publication restores the original operation; permission restoration still
// needs the normal source sync to publish that current content first.
func RecheckWikiSkippedOperations(
	ctx context.Context,
	db *gorm.DB,
	limit int,
) ([]WikiSkipRecheckTarget, error) {
	if limit < 1 || limit > 64 {
		return nil, errors.New("invalid Wiki skip recheck limit")
	}
	var records []types.TaskPendingOp
	if err := db.WithContext(ctx).
		Where("scope=? AND task_type='wiki:document_skip' AND op='skipped' AND enqueued_at<?",
			wikiDocumentSkipScope, time.Now().UTC().Add(-30*time.Minute)).Order("enqueued_at,id").
		Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	targets := []WikiSkipRecheckTarget{}
	seen := map[string]bool{}
	for _, record := range records {
		restored := int64(0)
		err := db.WithContext(types.WithLegacyKBAdminMetadata(ctx, record.ScopeID)).
			Transaction(func(tx *gorm.DB) error {
				var kb types.KnowledgeBase
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?",
					record.ScopeID, record.TenantID).First(&kb).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return tx.Model(&record).Update("enqueued_at", time.Now().UTC()).Error
					}
					return err
				}
				if !kb.IsWikiEnabled() {
					return tx.Model(&record).Update("enqueued_at", time.Now().UTC()).Error
				}
				_, err := wikiDependenciesSelected(tx, kb.ID, nil, map[string]bool{record.DedupKey: true})
				if err != nil {
					if !errors.Is(err, types.ErrLegacySourceRestricted) &&
						!errors.Is(err, ErrWikiMCPUnverified) &&
						!errors.Is(err, ErrWikiMCPInputsChanged) {
						return err
					}
					return tx.Model(&record).Update("enqueued_at", time.Now().UTC()).Error
				}
				result := tx.Model(&types.TaskPendingOp{}).
					Where("tenant_id=? AND scope='wiki_skipped_ops' AND scope_id=? AND "+
						"task_type='wiki:ingest' AND op='ingest' AND dedup_key=? AND "+
						"claimed_at IS NULL", record.TenantID, kb.ID, record.DedupKey).
					Where(wikiRestoreNoLiveDuplicate).
					Update("scope", "knowledge_base")
				if result.Error != nil {
					return result.Error
				}
				restored += result.RowsAffected
				result = tx.Model(&types.TaskPendingOp{}).
					Where("tenant_id=? AND scope='wiki_waiting_finalize' AND scope_id=? AND "+
						"task_type='wiki:finalize' AND claimed_at IS NULL", record.TenantID, kb.ID).
					Where(wikiRestoreNoLiveDuplicate).
					Update("scope", "knowledge_base")
				if result.Error != nil {
					return result.Error
				}
				restored += result.RowsAffected
				return writeWikiDocumentSkip(tx, record.TenantID, kb.ID, record.DedupKey, "")
			})
		if err != nil {
			return targets, err
		}
		if restored > 0 && !seen[record.ScopeID] {
			targets = append(
				targets,
				WikiSkipRecheckTarget{TenantID: record.TenantID, KnowledgeBaseID: record.ScopeID},
			)
			seen[record.ScopeID] = true
		}
	}
	return targets, nil
}

// selectWikiDocumentInputs is shared by the read-only operator preview and
// worker admission. It never updates diagnostics, pages or queue rows.
func selectWikiDocumentInputs(
	tx *gorm.DB,
	kb string,
	tenant uint64,
) (*WikiDocumentSelection, *wikiGenerationProof, error) {
	selection := &WikiDocumentSelection{Eligible: map[string]bool{}, Skipped: map[string]string{}}
	var proof *wikiGenerationProof
	var docs []struct{ ID, Channel, ParseStatus, EnableStatus string }
	if err := tx.Table("knowledges").Select("id,channel,parse_status,enable_status").
		Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Order("id").
		Limit(2049).Find(&docs).Error; err != nil {
		return nil, nil, err
	}
	if len(docs) > 2048 {
		return nil, nil, ErrWikiMCPUnverified
	}
	for _, doc := range docs {
		reason := ""
		switch {
		case doc.EnableStatus != "enabled":
			reason = "document_disabled"
		case doc.ParseStatus != "completed" && doc.ParseStatus != "finalizing":
			reason = "document_not_parsed"
		case doc.Channel == "feishu_personal":
			reason = "personal_publication_pending"
		default:
			_, err := wikiDependenciesSelected(tx, kb, nil, map[string]bool{doc.ID: true})
			if err != nil {
				if !errors.Is(err, types.ErrLegacySourceRestricted) &&
					!errors.Is(err, ErrWikiMCPUnverified) &&
					!errors.Is(err, ErrWikiMCPInputsChanged) {
					return nil, nil, err
				}
				reason = "source_verification_required"
			}
		}
		if reason != "" {
			selection.Skipped[doc.ID] = reason
		} else {
			selection.Eligible[doc.ID] = true
		}
	}
	if len(selection.Eligible) > 0 {
		var err error
		proof, err = wikiDependenciesSelected(tx, kb, nil, selection.Eligible)
		if err != nil {
			return nil, nil, err
		}
	} else {
		proof = &wikiGenerationProof{
			Tenant: tenant, KB: kb, Sources: map[string]string{},
			Epochs: map[string]string{}, SelectedSources: selection.Eligible,
		}
	}
	return selection, proof, nil
}
