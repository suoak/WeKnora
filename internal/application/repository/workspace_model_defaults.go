package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateDefaultSettings atomically compares the read snapshot, writes only
// the model-default column and appends the rollback pre-image to the audit.
func (r *tenantRepository) UpdateDefaultSettings(
	ctx context.Context,
	tenantID uint64,
	next, previous *types.DefaultSettings,
) error {
	if next == nil || previous == nil {
		return fmt.Errorf("model defaults and previous snapshot are required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant types.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tenant, "id = ?", tenantID).Error; err != nil {
			return err
		}
		current := types.DefaultSettings{}
		if tenant.DefaultSettings != nil {
			current = *tenant.DefaultSettings
		}
		if current != *previous {
			return types.ErrDefaultSettingsConflict
		}
		// Include a snapshot predicate as SQLite has no SELECT FOR UPDATE.
		query := tx.Model(&types.Tenant{}).Where("id = ?", tenantID)
		if tenant.DefaultSettings == nil {
			query = query.Where("default_settings IS NULL")
		} else {
			encoded, err := json.Marshal(current)
			if err != nil {
				return err
			}
			if tx.Dialector.Name() == "sqlite" {
				query = query.Where("CAST(default_settings AS TEXT) = ?", string(encoded))
			} else {
				query = query.Where("default_settings = ?", string(encoded))
			}
		}
		result := query.Updates(map[string]any{"default_settings": next, "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return types.ErrDefaultSettingsConflict
		}
		details, err := json.Marshal(map[string]any{"old_value": current, "new_value": next})
		if err != nil {
			return err
		}
		actor, _ := types.UserIDFromContext(ctx)
		return tx.Create(&types.AuditLog{
			TenantID: tenantID, ActorUserID: actor, ActorRole: string(types.TenantRoleFromContext(ctx)),
			Action: "model.workspace_defaults_changed", ScopeType: "workspace", ScopeID: fmt.Sprint(tenantID),
			Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: time.Now(),
		}).Error
	})
}
