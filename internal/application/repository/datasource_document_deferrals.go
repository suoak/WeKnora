package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func (r *DataSourceRepository) ReplaceDocumentDeferrals(
	ctx context.Context, source *types.DataSource, expected string, entries []types.SyncDocumentDeferral,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := lockSyncDataSource(tx, source.ID, source.TenantID)
		if err != nil {
			return err
		}
		if !legacySourceKind(current.Type) || current.KnowledgeBaseID != source.KnowledgeBaseID ||
			(current.Status != types.DataSourceStatusActive &&
				current.Status != types.DataSourceStatusError) ||
			current.LegacyAccessState == "hold" ||
			current.LegacyAccessState == "revoked" ||
			current.DeletedAt.Valid {
			return errors.New("source is not eligible for document policy changes")
		}
		var running int64
		if err := tx.Model(&types.SyncLog{}).Where("data_source_id=? AND tenant_id=? AND status=?",
			current.ID,
			current.TenantID,
			types.SyncLogStatusRunning).Count(&running).Error; err != nil {
			return err
		}
		if running != 0 {
			return errors.New("wait for the current source sync to finish before editing document policy")
		}
		old, err := current.ParseDocumentDeferrals()
		if err != nil {
			return err
		}
		if expected == "" || types.DocumentDeferralHash(old) != expected {
			return errors.New("document policy changed; reload before editing")
		}
		if entries == nil {
			entries = []types.SyncDocumentDeferral{}
		}
		blob, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		current.DocumentDeferrals = blob
		if _, err = current.ParseDocumentDeferrals(); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err = tx.Model(&types.DataSource{}).Where("id=? AND tenant_id=?", current.ID, current.TenantID).
			UpdateColumns(map[string]any{"document_deferrals": types.JSON(blob), "updated_at": now}).Error; err != nil {
			return err
		}
		source.DocumentDeferrals = blob
		return nil
	})
}
