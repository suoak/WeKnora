package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NormalSourceFromDatabase implements the normal enterprise service policy.
// Interactive callers need current write membership in the owning space. A
// background application-credential job is authorized by that space's current
// management, not by impersonating the user who originally created the source.
// Personal OAuth and private KBs remain bound to their actual owner.
func NormalSourceFromDatabase(
	ctx context.Context, db, publication *gorm.DB, source, config string,
) (NormalSourceAuthority, error) {
	var out NormalSourceAuthority
	safe, err := strconv.ParseBool(os.Getenv("FEISHU_SAFE_INCREMENTAL_ENABLED"))
	if err != nil || !safe || db.Name() != "postgres" {
		return out, ErrRecoveryRejected
	}
	work := db.WithContext(ctx)
	if publication != nil {
		work = publication.WithContext(ctx)
	}
	var ds types.DataSource
	if err := work.First(&ds, "id=?", source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, ErrRecoveryRejected
		}
		return out, err
	}
	if (ds.Type != "feishu" && ds.Type != "feishu_drive" && ds.Type != "lark" && ds.Type != "lark_drive") ||
		(ds.Status != "active" && ds.Status != "error") ||
		(ds.LegacyAccessState != "active" && ds.LegacyAccessState != "unmanaged") {
		return out, ErrRecoveryRejected
	}
	fingerprint, err := feishusafety.SourceConfigFingerprint(
		ds.ID, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.TenantID, ds.Config)
	if err != nil || fingerprint != config {
		return out, ErrRecoveryRejected
	}
	cfg, err := ds.ParseConfig()
	if err != nil || cfg == nil || len(cfg.ResourceIDs) == 0 || len(cfg.ResourceIDs) > 64 {
		return out, ErrRecoveryRejected
	}
	ref, err := types.ParseFeishuSourceReference(cfg)
	if err != nil {
		return out, err
	}
	var privateOwner string
	if err := work.Raw("SELECT coalesce((SELECT owner_id FROM feishu_private_kbs WHERE kb_id=?),'')",
		ds.KnowledgeBaseID).Scan(&privateOwner).Error; err != nil {
		return out, err
	}
	execution, claimed := types.SyncExecutionFromContext(ctx)
	identity, outbound := feishusafety.RecoveryOutboundExecution(ctx)
	actor, _ := types.UserIDFromContext(ctx)
	if types.IsSyntheticUserID(actor) {
		actor = ""
	}
	if !claimed && actor != "" {
		caller := types.CallerFromContext(ctx)
		if caller.TenantID != 0 && caller.TenantID != ds.TenantID {
			return out, ErrRecoveryRejected
		}
	}
	if ref != nil {
		if actor != "" && actor != ref.OwnerUserID {
			return out, ErrRecoveryRejected
		}
		actor = ref.OwnerUserID
	} else if actor == "" && privateOwner != "" {
		actor = privateOwner
	} else if actor == "" {
		if err := work.Raw(`SELECT m.user_id FROM tenant_members m
 JOIN users u ON u.id=m.user_id AND u.is_active=true AND u.deleted_at IS NULL
 WHERE m.tenant_id=? AND m.role IN ('owner','admin') AND m.status='active' AND m.deleted_at IS NULL
 ORDER BY CASE WHEN m.role='owner' THEN 0 ELSE 1 END,m.user_id LIMIT 1`, ds.TenantID).Scan(&actor).Error; err != nil {
			return out, err
		}
	}
	if actor == "" || (privateOwner != "" && privateOwner != actor) {
		return out, ErrRecoveryRejected
	}
	var authority struct {
		Role string
	}
	query := `SELECT m.role FROM tenant_members m
 JOIN users u ON u.id=m.user_id AND u.is_active=true AND u.deleted_at IS NULL
 JOIN tenants t ON t.id=m.tenant_id AND t.status='active' AND t.deleted_at IS NULL
 JOIN knowledge_bases k ON k.id=? AND k.tenant_id=t.id AND k.deleted_at IS NULL
 WHERE m.user_id=? AND m.tenant_id=? AND m.status='active' AND m.deleted_at IS NULL
 AND m.role IN ('owner','admin','contributor','viewer')`
	if publication != nil {
		query += " FOR SHARE OF m,u,t,k"
	}
	checked := work.Raw(query, ds.KnowledgeBaseID, actor, ds.TenantID).Scan(&authority)
	if checked.Error != nil {
		return out, checked.Error
	}
	if checked.RowsAffected != 1 {
		return out, ErrRecoveryRejected
	}
	if claimed {
		if execution.DataSourceID != source || execution.TenantID != ds.TenantID || execution.Generation < 1 ||
			(outbound && (identity.Run != execution.SyncLogID || identity.Generation != execution.Generation)) {
			return out, ErrRecoveryRejected
		}
		var live int64
		if err := work.Model(&types.SyncLog{}).Where(
			"id=? AND data_source_id=? AND tenant_id=? AND status='running' AND execution_claimed=true "+
				"AND execution_generation=?", execution.SyncLogID, source, ds.TenantID, execution.Generation).
			Count(&live).Error; err != nil {
			return out, err
		}
		if live != 1 {
			return out, ErrRecoveryRejected
		}
	} else if outbound {
		return out, ErrRecoveryRejected
	}
	var guards int64
	if err := work.Raw(`SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgenabled='O'
 AND tgname IN ('feishu_legacy_knowledge_fence','feishu_legacy_chunk_fence','feishu_legacy_embedding_fence')`).
		Scan(&guards).Error; err != nil {
		return out, err
	}
	if guards != 3 {
		return out, ErrRecoveryRejected
	}
	digest, err := RecoveryDigest([]any{
		source, ds.TenantID, ds.KnowledgeBaseID, actor, authority.Role, privateOwner, fingerprint,
	})
	if err != nil {
		return out, err
	}
	// Operational edits need no new service signature, but must invalidate an
	// in-flight execution's old deletion/mode/document-policy snapshot.
	executionHash, err := RecoveryDigest([]any{fingerprint, ds.SyncMode, ds.SyncDeletions, ds.DocumentDeferrals})
	return NormalSourceAuthority{
		SourceID: source, TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID,
		ConfigSHA256: fingerprint, AuthoritySHA256: digest, ExecutionSHA256: executionHash,
	}, err
}

// StoreNormalServiceRecord keeps the signed boundary and its history durable.
// It never changes source state, credentials, task identity, or synchronization cursors.
func StoreNormalServiceRecord(
	ctx context.Context, db *gorm.DB, record NormalServiceRecord, previous *NormalServiceRecord,
) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.SystemSetting
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "key=?", NormalServiceSetting).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if previous != nil {
				return ErrRecoveryRejected
			}
		} else if err != nil {
			return err
		} else {
			var parsed NormalServiceRecord
			if previous == nil || json.Unmarshal(current.Value, &parsed) != nil || parsed != *previous {
				return ErrRecoveryRejected
			}
			history := current
			history.ID, history.Key = 0, NormalServiceSetting+".history."+uuid.NewString()
			if err := tx.Create(&history).Error; err != nil {
				return err
			}
		}
		value, err := json.Marshal(record)
		if err != nil {
			return err
		}
		current.Key, current.Value = NormalServiceSetting, types.JSON(value)
		current.ValueType, current.Category = "string", "integration"
		current.LastModifiedBy = record.Authorization.ActorID
		if current.ID == 0 {
			return tx.Create(&current).Error
		}
		return tx.Model(&current).Updates(map[string]any{
			"value": current.Value, "last_modified_by": current.LastModifiedBy,
		}).Error
	})
}
