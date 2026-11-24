package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// LoadPortalMCPMetadata projects only authorized IDs, in bounded batches. It
// neither enumerates users nor loads document bodies or global asset counts.
func (r *EnterpriseAccessRepository) LoadPortalMCPMetadata(
	ctx context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	out := make(map[string]access.PortalKBMetadata, len(targets))
	for start := 0; start < len(targets); start += 500 {
		batch := targets[start:min(start+500, len(targets))]
		ids := make([]string, 0, len(batch))
		for _, target := range batch {
			ids = append(ids, target.Descriptor().ID)
		}
		var rows []struct {
			access.PortalKBMetadata `gorm:"embedded"`
			TenantID                uint64
			Username                string
		}
		err := r.db.WithContext(ctx).Table("knowledge_bases kb").
			Select("kb.id,kb.tenant_id,kb.description,kb.created_at,kb.updated_at,kb.type,"+
				"COALESCE(u.username,'') AS username").
			Joins("LEFT JOIN users u ON u.id=kb.creator_id AND u.deleted_at IS NULL").
			Where("kb.id IN ? AND kb.deleted_at IS NULL", ids).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		owners := make(map[string]uint64, len(batch))
		for _, target := range batch {
			d := target.Descriptor()
			owners[d.ID] = d.TenantID
		}
		for _, row := range rows {
			if owners[row.ID] != row.TenantID {
				return nil, access.ErrForbidden
			}
			metadata := row.PortalKBMetadata
			metadata.CreatorName = types.UserDisplayName(&types.User{Username: row.Username})
			out[row.ID] = metadata
		}
	}
	return out, nil
}
