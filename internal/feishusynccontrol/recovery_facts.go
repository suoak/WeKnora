package feishusynccontrol

import (
	"context"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// RecoveryFactsFromDatabase reads the same authoritative snapshot used at
// plan/reset/admission and by the running canary watcher. It grants no content
// read permission and performs no source/authorization updates.
func RecoveryFactsFromDatabase(ctx context.Context, db *gorm.DB, id string) (RecoveryFacts, error) {
	var f RecoveryFacts
	var ds types.DataSource
	if err := db.WithContext(ctx).Where("id=?", id).First(&ds).Error; err != nil {
		return f, err
	}
	f.SourceID = id
	fp, err := feishusafety.SourceConfigFingerprint(
		ds.ID,
		ds.KnowledgeBaseID,
		ds.Type,
		ds.LegacyAccessState,
		ds.TenantID,
		ds.Config,
	)
	if err != nil {
		return f, err
	}
	f.ConfigSHA256 = fp
	legacy := ds.Type == "feishu" || ds.Type == "feishu_drive" || ds.Type == "lark" || ds.Type == "lark_drive"
	f.Eligible = legacy && (ds.Status == "active" || ds.Status == "error") &&
		(ds.LegacyAccessState == "active" || ds.LegacyAccessState == "unmanaged")
	var liveKB int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM knowledge_bases k JOIN tenants t ON t.id=k.tenant_id
 WHERE k.id=? AND k.tenant_id=? AND k.deleted_at IS NULL AND t.deleted_at IS NULL AND t.status='active'`,
		ds.KnowledgeBaseID,
		ds.TenantID).
		Scan(&liveKB).
		Error; err != nil {
		return f, err
	}
	f.Eligible = f.Eligible && liveKB == 1
	if execution, claimed := types.SyncExecutionFromContext(ctx); claimed {
		if execution.DataSourceID != id || execution.TenantID != ds.TenantID || execution.Generation < 1 {
			f.Eligible = false
		} else {
			var current int64
			if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM sync_logs
 WHERE id=? AND data_source_id=? AND tenant_id=? AND execution_generation=?
 AND execution_claimed=true AND status='running'`, execution.SyncLogID, id, ds.TenantID, execution.Generation).
				Scan(&current).Error; err != nil {
				return f, err
			}
			f.Eligible = f.Eligible && current == 1
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM sync_logs s JOIN data_sources d ON d.id=s.data_source_id
 WHERE d.type IN ('feishu',
'feishu_drive',
'lark',
'lark_drive') AND s.execution_claimed=true`).
		Scan(&f.LiveExecutions).
		Error; err != nil {
		return f, err
	}
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM feishu_legacy_attempts a
JOIN feishu_legacy_resources r ON r.id=a.resource_id
 WHERE a.id=r.current_attempt AND a.generation=r.generation AND a.status IN ('ready','staging') AND
a.lease_until>now()`).Scan(&f.LivePublications).Error; err != nil {
		return f, err
	}
	var guards int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgenabled='O'
 AND tgname IN ('feishu_legacy_knowledge_fence',
'feishu_legacy_chunk_fence',
'feishu_legacy_embedding_fence')`).
		Scan(&guards).
		Error; err != nil {
		return f, err
	}
	f.Eligible = f.Eligible && guards == 3
	var membership []map[string]any
	if err := db.WithContext(ctx).
		Table("tenant_members tm").
		Select("tm.user_id,tm.role,tm.status,tm.deleted_at,u.is_active,u.deleted_at AS user_deleted_at").
		Joins("JOIN users u ON u.id=tm.user_id").
		Where("tm.tenant_id=?",
			ds.TenantID).
		Order("tm.user_id,tm.id").
		Find(&membership).
		Error; err != nil {
		return f, err
	}
	var owners []map[string]any
	if err := db.WithContext(ctx).
		Table("feishu_private_kbs").
		Select("kb_id,owner_id").
		Where("kb_id=?",
			ds.KnowledgeBaseID).
		Order("kb_id").
		Find(&owners).
		Error; err != nil {
		return f, err
	}
	var identity []map[string]any
	if err := db.WithContext(ctx).
		Table("system_settings").
		Select("key,updated_at,md5(value::text) AS digest").
		Where("key=?",
			"integration.feishu.personal_app").
		Find(&identity).
		Error; err != nil {
		return f, err
	}
	var epochs []map[string]any
	var connections []map[string]any
	cfg, err := ds.ParseConfig()
	if err != nil {
		return f, err
	}
	f.SelectedResourceCount = len(cfg.ResourceIDs)
	ref, err := types.ParseFeishuSourceReference(cfg)
	if err != nil {
		return f, err
	}
	if ref != nil && ref.OwnerUserID != "" {
		var writers int64
		if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM tenant_members tm JOIN users u ON u.id=tm.user_id
   WHERE tm.tenant_id=? AND tm.user_id=? AND tm.status='active' AND tm.deleted_at IS NULL
   AND tm.role IN ('owner',
'admin',
'contributor') AND u.is_active=true AND u.deleted_at IS NULL`,
			ds.TenantID,
			ref.OwnerUserID).
			Scan(&writers).
			Error; err != nil {
			return f, err
		}
		f.Eligible = f.Eligible && writers == 1

		if err := db.WithContext(ctx).
			Table("feishu_personal_grant_epochs").
			Select("user_id,epoch").
			Where("user_id=?",
				ref.OwnerUserID).
			Find(&epochs).
			Error; err != nil {
			return f, err
		}
		if ref.Kind == types.FeishuUserOAuth {
			if err := db.WithContext(ctx).
				Table("feishu_connections").
				Select("user_id,app_id,open_id").
				Where("user_id=? AND app_id=?",
					ref.OwnerUserID,
					ref.AppID).
				Find(&connections).
				Error; err != nil {
				return f, err
			}
			f.Eligible = f.Eligible && len(connections) == 1 && len(epochs) == 1
		}

	}
	f.FactsSHA256, err = RecoveryDigest([]any{fp, membership, owners, identity, epochs, connections})
	// Preserve the ordinary source retry policy after approval. A transient
	// execution error is not a permission revocation; pause/hold/revoked and
	// every other authority condition above remain closed. New recovery plans
	// still require the source to be active.
	f.RetryEligible = f.Eligible && ds.Status == "error"
	f.Eligible = f.Eligible && ds.Status == "active"
	return f, err
}
