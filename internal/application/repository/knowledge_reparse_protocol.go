package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// PersonalReparseMetadata contains identity fields, never document content.
type PersonalReparseMetadata struct {
	ID, KnowledgeBaseID string
	TenantID            uint64
	LegacySourceManaged bool
	LegacyResourceBound bool
	DataSourceID        string
}

// Protocol metadata is deliberately projected without loading Knowledge or
// invoking a body read hook. It remains available when recovery is disabled.
func (r *knowledgeRepository) LoadPersonalReparseMetadata(
	ctx context.Context, id string,
) (*PersonalReparseMetadata, error) {
	var row PersonalReparseMetadata
	legacy := "(k.channel IN ('feishu','feishu_drive','lark','lark_drive') " +
		"AND COALESCE(k.metadata->>'datasource_id','')<>'' AND COALESCE(k.metadata->>'external_id','')<>'')"
	resourceBound := "false"
	if types.LegacyReadGuardsEnabled(r.db) {
		resourceBound = "EXISTS(SELECT 1 FROM feishu_legacy_resources lr WHERE lr.knowledge_id=k.id)"
		legacy = "(" + legacy + " OR " + resourceBound + ")"
	}
	projection := "k.id,k.tenant_id,k.knowledge_base_id," + legacy +
		" AS legacy_source_managed," + resourceBound + " AS legacy_resource_bound," +
		"COALESCE(k.metadata->>'datasource_id','') AS data_source_id"
	err := r.db.WithContext(ctx).Table("knowledges AS k").Select(projection).
		Where("k.id=? AND k.deleted_at IS NULL AND (EXISTS("+
			"SELECT 1 FROM task_pending_ops op WHERE op.scope=? AND op.scope_id=k.id "+
			"AND op.tenant_id=k.tenant_id AND op.payload->>'payload_version' IN ('3','5')) "+
			"OR EXISTS(SELECT 1 FROM task_pending_ops receipt WHERE receipt.scope=? AND receipt.scope_id=k.id "+
			"AND receipt.tenant_id=k.tenant_id) OR "+legacy+")", id,
			types.KnowledgeDispatchScope, types.PersonalExactReceiptScope).
		Scan(&row).Error
	if err == nil && row.LegacyResourceBound {
		var source struct{ DataSourceID string }
		err = r.db.WithContext(ctx).Table("feishu_legacy_resources").Select("data_source_id").
			Where("knowledge_id=? AND tenant_id=? AND knowledge_base_id=?", row.ID, row.TenantID, row.KnowledgeBaseID).
			Limit(1).Scan(&source).Error
		if err == nil {
			row.DataSourceID = source.DataSourceID
		}
	}
	return &row, err
}
