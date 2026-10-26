package service

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrFeishuSourceRebindRequired distinguishes a stale persisted source reference
// from an expired token or missing document permission. Reauthorizing again does
// not migrate a source and must never silently revive its old grant.
var ErrFeishuSourceRebindRequired = errors.New("feishu_source_identity_rebind_required")

func validatePersonalSourceBinding(tx *gorm.DB, ref *types.FeishuSourceReference) error {
	if ref == nil || ref.Kind != types.FeishuUserOAuth {
		return nil
	}
	var connection types.FeishuConnection
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ? AND app_id = ?",
		ref.OwnerUserID, ref.AppID).First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFeishuReconnect
		}
		return err
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ?", ref.OwnerUserID).
		First(&epoch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFeishuReconnect
		}
		return err
	}
	current := connectionID(ref.Kind, ref.OwnerUserID, ref.AppID, connection.OpenID, epoch.Epoch)
	if ref.ConnectionID == "" || ref.ConnectionID != current {
		return ErrFeishuSourceRebindRequired
	}
	return nil
}
