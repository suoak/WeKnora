package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RetainVerifiedVersion records a still-valid published snapshot after a newer
// fetched document failed staging. It does not publish that candidate, change
// its revision watermark, or claim that the failed document was updated.
// Callers must have successfully fetched this exact resource in the current
// claimed execution; permission/refusal items must never use this path.
func (s *FeishuLegacyStore) RetainVerifiedVersion(
	ctx context.Context, ds *types.DataSource, item *types.FetchedItem,
) (*types.FeishuLegacyReceipt, error) {
	if item == nil || item.IsDeleted || len(item.Content) == 0 || item.ExternalID == "" ||
		item.SourceResourceID == "" || item.UpdatedAt.IsZero() || item.Metadata["error"] != "" {
		return nil, legacyFence("retained_version_current_fetch_unconfirmed")
	}
	if err := types.CheckFeishuSourceRead(ctx); err != nil {
		return nil, err
	}
	var resource types.FeishuLegacyResource
	var attempt types.FeishuLegacyAttempt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(tx, ctx, ds); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&resource,
			"id=? AND tenant_id=? AND knowledge_base_id=? AND data_source_id=? AND external_id=? AND state='active'",
			legacyResourceID(ds, item.ExternalID), ds.TenantID, ds.KnowledgeBaseID, ds.ID, item.ExternalID).Error; err != nil {
			return err
		}
		if resource.ActiveAttempt == "" {
			return legacyFence("retained_version_missing_proof")
		}
		if err := tx.First(&attempt, "id=? AND resource_id=? AND status='published'",
			resource.ActiveAttempt, resource.ID).Error; err != nil {
			return err
		}
		previous, err := time.Parse(time.RFC3339Nano, attempt.Revision)
		if err != nil || item.UpdatedAt.Before(previous) {
			return legacyFence("retained_version_revision_regressed")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Receipt verifies the immutable manifest, current chunks, assets, vectors
	// and public binding. Cursor commit rechecks the active receipt atomically.
	return s.Receipt(ctx, &resource, &attempt)
}

// UnpublishedResource has no previously imported content to revoke. A refusal
// for this exact new document cannot invalidate unrelated published resources.
func (s *FeishuLegacyStore) UnpublishedResource(
	ctx context.Context, ds *types.DataSource, external string,
) (bool, error) {
	if external == "" {
		return false, legacyFence("restriction_identity_missing")
	}
	var empty bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.executionFence(ctx, tx, ds); err != nil {
			return err
		}
		var existing int64
		if err := tx.Table("knowledges").Where(
			"tenant_id=? AND knowledge_base_id=? AND metadata->>'datasource_id'=? AND metadata->>'external_id'=?",
			ds.TenantID, ds.KnowledgeBaseID, ds.ID, external).Count(&existing).Error; err != nil {
			return err
		}
		var enrolled int64
		if err := tx.Model(&types.FeishuLegacyResource{}).Where("id=?", legacyResourceID(ds, external)).
			Count(&enrolled).Error; err != nil {
			return err
		}
		empty = existing == 0 && enrolled == 0
		return nil
	})
	return empty, err
}
