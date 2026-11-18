package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const wikiPublishedSnapshotScope = "wiki_published_snapshot"

// Capture the exact published row under the existing page write lock. This is
// browser presentation state, never an alternate generation input or a grant.
func retainWikiPublishedSnapshot(ctx context.Context, tx *gorm.DB, previous, next *types.WikiPage) error {
	if preservingGeneration(ctx) == nil || previous.Status != types.WikiPageStatusPublished ||
		next.Status != types.WikiPageStatusDraft {
		return nil
	}
	if tx.Name() != "postgres" {
		return errors.New("published Wiki snapshot requires PostgreSQL")
	}
	var count int64
	if err := wikiPublishedSnapshots(tx, previous.TenantID, previous.KnowledgeBaseID, previous.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrWikiPageConflict
	}
	var payload []byte
	if err := tx.Raw(`SELECT jsonb_build_object('page',to_jsonb(p),
 'row_digest',md5(to_jsonb(p)::text)) FROM wiki_pages p
 WHERE p.id=? AND p.tenant_id=? AND p.knowledge_base_id=? AND p.deleted_at IS NULL AND p.status='published'`,
		previous.ID, previous.TenantID, previous.KnowledgeBaseID).Row().Scan(&payload); err != nil {
		return err
	}
	return tx.Create(&types.TaskPendingOp{
		TenantID: previous.TenantID, TaskType: "wiki:published_snapshot", Scope: wikiPublishedSnapshotScope,
		ScopeID: previous.KnowledgeBaseID, DedupKey: previous.ID, Op: "retained",
		Payload: payload, EnqueuedAt: time.Now().UTC(),
	}).Error
}

func wikiPublishedSnapshots(db *gorm.DB, tenant uint64, kb, page string) *gorm.DB {
	return db.Model(&types.TaskPendingOp{}).Where(
		"tenant_id=? AND scope=? AND scope_id=? AND task_type=? AND dedup_key=?",
		tenant, wikiPublishedSnapshotScope, kb, "wiki:published_snapshot", page)
}

// Successful publication or an explicit archive retires the old presentation
// in the SAME transaction as the guarded output. Rollback retains both sides.
func retireWikiPublishedSnapshot(tx *gorm.DB, page *types.WikiPage) error {
	if page.Status == types.WikiPageStatusDraft || tx.Name() != "postgres" {
		return nil
	}
	return wikiPublishedSnapshots(tx, page.TenantID, page.KnowledgeBaseID, page.ID).
		Delete(&types.TaskPendingOp{}).Error
}

// Only interactive, tenant-bound readers see the retained row. SQL filtering,
// search, counts and pagination operate on that exact old row, not on the draft.
// Keep the canonical table alias so all existing source/KB/AfterFind guards run.
// Model and background readers always use the actual candidate page and proof.
func wikiBrowserPageDB(ctx context.Context, db *gorm.DB) *gorm.DB {
	tenant := types.WorkspaceWebReadTenant(ctx)
	_, generating := WikiMCPGenerationTenant(ctx)
	if tenant == 0 || generating || db.Name() != "postgres" {
		return db.WithContext(ctx)
	}
	return db.WithContext(ctx).Table(`(
 SELECT (jsonb_populate_record(NULL::wiki_pages,
 CASE WHEN p.status='draft' AND s.n=1
  AND s.payload->'page'->>'id'=p.id
  AND s.payload->'page'->>'knowledge_base_id'=p.knowledge_base_id
  AND (s.payload->'page'->>'tenant_id')::text=p.tenant_id::text
  AND s.payload->'page'->>'status'='published'
  AND s.payload->'page'->>'slug'=p.slug
  AND s.payload->'page'->>'deleted_at' IS NULL
  AND md5((s.payload->'page')::text)=s.payload->>'row_digest'
 THEN s.payload->'page' ELSE to_jsonb(p) END)).*
 FROM wiki_pages p
 LEFT JOIN LATERAL (SELECT payload,count(*) OVER() AS n FROM task_pending_ops
  WHERE tenant_id=p.tenant_id AND scope='wiki_published_snapshot'
   AND scope_id=p.knowledge_base_id AND dedup_key=p.id
   AND task_type='wiki:published_snapshot' AND op='retained' LIMIT 1) s ON true
 WHERE p.tenant_id=? AND p.deleted_at IS NULL
) AS wiki_pages`, tenant)
}
