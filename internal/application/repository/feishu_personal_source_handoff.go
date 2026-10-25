package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HandoffSource fences the old first-create attempt before its stable Knowledge
// is enrolled in the existing datasource version publisher. No cursor, file or
// index is removed. A live worker lease prevents the handoff.
func (r *PersonalRecoveryRepository) HandoffSource(
	ctx context.Context,
	id, user string,
	fn func(*gorm.DB, *types.FeishuPersonalImport) error,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m types.FeishuPersonalImport
		var resource types.FeishuPersonalResource
		var attempt types.FeishuPersonalAttempt
		if err := tx.Where("id = ? AND user_id = ?", id, user).First(&m).Error; err != nil {
			return err
		}
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("import_id = ?",
				id).
			First(&resource).Error; err != nil {
			return err
		}
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?",
				resource.CurrentAttemptID).
			First(&attempt).Error; err != nil {
			return err
		}
		m = types.FeishuPersonalImport{}
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?",
				id,
				user).
			First(&m).Error; err != nil {
			return err
		}
		if m.Status == "source_managed" {
			return nil
		}
		if m.Status == "paused" || m.Status == "hold" || m.Status == "revoked" ||
			attempt.LeaseUntil != nil && attempt.LeaseUntil.After(time.Now()) {
			return ErrPersonalBusy
		}
		if m.Status != "indexed" && m.Status != "synced" && m.Status != "completed" {
			return ErrPersonalIdentity
		}
		if m.KnowledgeID == "" || attempt.LogicalKnowledgeID != m.KnowledgeID ||
			attempt.Status == "obsolete" ||
			attempt.Status == "needs_attention" {
			return ErrPersonalIdentity
		}
		if err := r.lockPrincipal(tx, &m, &attempt); err != nil {
			return err
		}
		if err := fn(tx, &m); err != nil {
			return err
		}
		resource.Generation++
		if err := tx.Save(&resource).Error; err != nil {
			return err
		}
		if err := tx.Model(&attempt).Update("status", "obsolete").Error; err != nil {
			return err
		}
		return tx.Model(&m).
			Updates(map[string]any{"status": "source_managed", "updated_at": time.Now()}).
			Error
	})
}
