package repository

import (
	"context"
	"github.com/Tencent/WeKnora/internal/application/access"
)

// Enumeration never grants access. Both public and independent private branches
// use live policy checks before descriptor metadata can leave the resolver.
func (r *EnterpriseAccessRepository) ListPortalMCPCandidates(ctx context.Context) ([]access.PortalKBDescriptor, error) {
	rows := []access.PortalKBDescriptor{}
	err := r.db.WithContext(ctx).Table("knowledge_bases kb").
		Select("kb.tenant_id,kb.id,kb.name,t.name AS space_name,kb.embedding_model_id").
		Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND t.status='active'").
		Where("kb.deleted_at IS NULL AND kb.is_temporary=false AND kb.type='document'").
		Order("kb.tenant_id,kb.id").Find(&rows).Error
	return rows, err
}
