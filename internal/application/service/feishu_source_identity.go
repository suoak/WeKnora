package service

import (
	"context"
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

// SourceBindingAttention is owner-scoped, read-only inventory. OAuth status can
// surface stale references immediately after consent without invoking a worker,
// modifying sources, or treating a normal token refresh as reauthorization.
func (s *FeishuPersonalService) SourceBindingAttention(ctx context.Context) (int, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return 0, nil
	}
	if !s.db.Migrator().HasTable(&types.DataSource{}) || !s.db.Migrator().HasTable(&types.FeishuPersonalGrantEpoch{}) {
		return 0, nil
	}
	var rows []types.DataSource
	if err := s.db.WithContext(ctx).Where("id IN (?)", s.db.Model(&types.FeishuPersonalImport{}).
		Select("id").Where("user_id = ? AND auth_mode = ?", user, types.FeishuUserOAuth)).Find(&rows).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, row := range rows {
		config, err := row.ParseConfig()
		if err != nil {
			return 0, err
		}
		ref, err := types.ParseFeishuSourceReference(config)
		if err != nil {
			return 0, err
		}
		if ref == nil || ref.OwnerUserID != user || ref.Kind != types.FeishuUserOAuth {
			continue
		}
		err = validatePersonalSourceBinding(s.db.WithContext(ctx), ref)
		if errors.Is(err, ErrFeishuSourceRebindRequired) || errors.Is(err, ErrFeishuReconnect) {
			count++
		} else if err != nil {
			return 0, err
		}
	}
	return count, nil
}
