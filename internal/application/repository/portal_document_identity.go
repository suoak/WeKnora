package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// LookupPortalDocumentKB selects identity metadata for normal live permission
// resolution. It returns no document body, path, source credential or grant.
func (r *EnterpriseAccessRepository) LookupPortalDocumentKB(ctx context.Context, id string) (string, error) {
	var row struct{ KnowledgeBaseID string }
	err := r.db.WithContext(ctx).Table("knowledges").Select("knowledge_base_id").
		Where("id=? AND deleted_at IS NULL", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, types.ErrPrivateKnowledgeBase) ||
		errors.Is(err, types.ErrLegacySourceRestricted) || errors.Is(err, types.ErrExactDocumentScope) {
		return "", access.ErrForbidden
	}
	return row.KnowledgeBaseID, err
}
