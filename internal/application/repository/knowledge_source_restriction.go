package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Only identifiers/existence and source policy are queried. No Knowledge,
// source config, file, Chunk, or embedding content is loaded, and no read or
// publication capability is created by this operational admission check.
func knowledgeSourceRestriction(ctx context.Context, db *gorm.DB, tenant uint64, kb, kid string) (types.KnowledgeSourceRestriction, error) {
	status := types.UnknownKnowledgeSourceRestriction()
	if !types.LegacyReadGuardsEnabled(db) {
		return status, nil
	}
	if tenant == 0 || kb == "" {
		return status, gorm.ErrRecordNotFound
	}
	var valid bool
	query := `SELECT EXISTS(SELECT 1 FROM knowledge_bases k JOIN tenants t ON t.id=k.tenant_id
WHERE k.id=? AND k.tenant_id=? AND k.deleted_at IS NULL AND t.deleted_at IS NULL AND t.status='active')`
	if err := db.WithContext(ctx).Raw(query, kb, tenant).Scan(&valid).Error; err != nil {
		return status, err
	}
	if !valid {
		return status, gorm.ErrRecordNotFound
	}
	if kid != "" {
		if err := db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM knowledges
WHERE id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL)`, kid, tenant, kb).Scan(&valid).Error; err != nil {
			return status, err
		}
		if !valid {
			return status, gorm.ErrRecordNotFound
		}
	}
	var policy struct{ Held, Revoked bool }
	err := db.WithContext(ctx).Raw(`SELECT
 EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id=? AND state='hold')
 OR EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND legacy_access_state='hold') AS held,
 EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id=? AND state='revoked')
 OR EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id=? AND legacy_access_state='revoked') AS revoked`, kb, kb, kb, kb).Scan(&policy).Error
	if err != nil {
		return status, err
	}
	status.SourceState = "available"
	if policy.Revoked {
		status.SourceState = "revoked"
		status.ReasonCode = "SOURCE_REVOKED"
	} else if policy.Held {
		status.SourceState = "hold"
		status.ReasonCode = "SOURCE_VERIFICATION_REQUIRED"
	}
	status.Restricted = policy.Held || policy.Revoked
	return status, nil
}

func (r *knowledgeRepository) KnowledgeSourceRestriction(ctx context.Context, tenant uint64, kb, kid string) (types.KnowledgeSourceRestriction, error) {
	return knowledgeSourceRestriction(ctx, r.db, tenant, kb, kid)
}

func (r *DataSourceRepository) KnowledgeSourceRestriction(ctx context.Context, tenant uint64, kb, kid string) (types.KnowledgeSourceRestriction, error) {
	return knowledgeSourceRestriction(ctx, r.db, tenant, kb, kid)
}
