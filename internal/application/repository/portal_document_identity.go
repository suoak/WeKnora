package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"gorm.io/gorm"
)

// LookupPortalDocumentKB selects identity metadata for normal live permission
// resolution. It returns no document body, path, source credential or grant.
func (r *EnterpriseAccessRepository) LookupPortalDocumentKB(ctx context.Context, id string) (string, error) {
	var row struct{ KnowledgeBaseID string }
	err := r.db.WithContext(ctx).Table("knowledges").Select("knowledge_base_id").
		Where("id=? AND deleted_at IS NULL", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", access.ErrForbidden
	}
	return row.KnowledgeBaseID, err
}
