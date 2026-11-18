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

// WikiPublishedSnapshotPlan binds browser-only recovery to the original backup and current rows.
type WikiPublishedSnapshotPlan struct {
	TenantID        uint64            `json:"tenant_id"`
	KnowledgeBaseID string            `json:"knowledge_base_id"`
	Restore         []string          `json:"restore"`
	Skipped         map[string]string `json:"skipped"`
	Snapshot        string            `json:"snapshot"`
	Applied         bool              `json:"applied"`
}

// RestoreWikiPublishedSnapshots reinstates only browser presentation from the
// original recovery backup. The operator marker's original row digest AND the
// exact published revision must match. It never publishes the current draft,
// certifies historical model inputs, edits a page or starts another generation.
func RestoreWikiPublishedSnapshots(ctx context.Context, db *gorm.DB, tenant uint64, kb string,
	originals []json.RawMessage, expected string, apply bool,
) (*WikiPublishedSnapshotPlan, error) {
	if db.Name() != "postgres" || tenant == 0 || kb == "" || len(originals) == 0 ||
		len(originals) > wikiMCPMaxPages || apply && expected == "" {
		return nil, errors.New("bounded original Wiki backup and exact scope required")
	}
	plan := &WikiPublishedSnapshotPlan{
		TenantID: tenant, KnowledgeBaseID: kb, Restore: []string{}, Skipped: map[string]string{},
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var markers []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=? AND task_type=? AND op='active'",
			wikiPreservingScope, kb, tenant, "wiki:preserve_recovery").Limit(2).Find(&markers).Error; err != nil {
			return err
		}
		if len(markers) != 1 {
			return errors.New("one active original recovery marker required")
		}
		var recovery WikiPreservingPlan
		if json.Unmarshal(markers[0].Payload, &recovery) != nil || !recovery.Applied ||
			recovery.TenantID != tenant || recovery.KnowledgeBaseID != kb ||
			recovery.Snapshot != markers[0].DedupKey || len(recovery.SourceSHA256) != 64 ||
			len(recovery.OriginalPages) != len(originals) || recovery.HistoricalPages != len(originals) {
			return ErrWikiMCPUnverified
		}
		seen := map[string]bool{}
		rows := map[string]json.RawMessage{}
		fingerprints := map[string]string{}
		for _, raw := range originals {
			var old types.WikiPage
			if json.Unmarshal(raw, &old) != nil || old.ID == "" || seen[old.ID] ||
				old.TenantID != tenant || old.KnowledgeBaseID != kb || old.DeletedAt.Valid ||
				recovery.OriginalPages[old.ID] != wikiDigest(&old) {
				return errors.New("backup differs from the original recovery snapshot")
			}
			seen[old.ID] = true
			if old.Status != types.WikiPageStatusPublished {
				plan.Skipped[old.ID] = "original_not_published"
				continue
			}
			var current types.WikiPage
			err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS snapshot_repair_current").
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", old.ID, tenant, kb).
				Take(&current).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				plan.Skipped[old.ID] = "page_removed"
				continue
			}
			if err != nil {
				return err
			}
			fingerprints[old.ID] = wikiDigest(&current)
			if current.Status != types.WikiPageStatusDraft || current.Slug != old.Slug ||
				current.Version <= old.Version ||
				current.LastEditSource != types.WikiEditSourcePipeline {
				plan.Skipped[old.ID] = "not_a_pipeline_draft_replacement"
				continue
			}
			var revisions []types.WikiPageRevision
			if err := tx.Session(&gorm.Session{SkipHooks: true}).
				Table("wiki_page_revisions AS snapshot_repair_history").
				Where("page_id=? AND tenant_id=? AND knowledge_base_id=? AND version=?",
					old.ID, tenant, kb, old.Version).Limit(2).Find(&revisions).Error; err != nil {
				return err
			}
			if len(revisions) != 1 || revisions[0].Status != old.Status || revisions[0].Slug != old.Slug ||
				revisions[0].Title != old.Title || revisions[0].Content != old.Content ||
				revisions[0].Summary != old.Summary || revisions[0].PageType != old.PageType ||
				wikiDigest(revisions[0].Aliases) != wikiDigest(old.Aliases) {
				plan.Skipped[old.ID] = "published_revision_differs"
				continue
			}
			var count int64
			if err := wikiPublishedSnapshots(tx, tenant, kb, old.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				plan.Skipped[old.ID] = "snapshot_already_present"
				continue
			}
			plan.Restore = append(plan.Restore, old.ID)
			rows[old.ID] = raw
		}
		sort.Strings(plan.Restore)
		plan.Snapshot = wikiDigest([]any{markers[0], fingerprints, plan.Restore, plan.Skipped})
		if !apply {
			return nil
		}
		if plan.Snapshot != expected {
			return errors.New("published snapshot repair changed; preview again")
		}
		for _, id := range plan.Restore {
			var payload []byte
			if err := tx.Raw(`SELECT jsonb_build_object('page',?::jsonb,
 'row_digest',md5((?::jsonb)::text))`, string(rows[id]), string(rows[id])).Row().Scan(&payload); err != nil {
				return err
			}
			if err := tx.Create(&types.TaskPendingOp{
				TenantID: tenant, TaskType: "wiki:published_snapshot", Scope: wikiPublishedSnapshotScope,
				ScopeID: kb, DedupKey: id, Op: "retained", Payload: payload, EnqueuedAt: time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
