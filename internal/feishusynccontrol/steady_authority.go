package feishusynccontrol

import (
	"context"
	"database/sql"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// SourceManagementAuthority is content-free, live authority for one source.
// A platform-admin flag is deliberately not an authorization input.
type SourceManagementAuthority struct {
	TenantID      uint64 `json:"tenant_id"`
	KBID          string `json:"knowledge_base_id"`
	SourceID      string `json:"source_id"`
	SourceType    string `json:"source_type"`
	SourceStatus  string `json:"source_status"`
	AccessState   string `json:"access_state"`
	ActorID       string `json:"actor_id"`
	AuthorityHash string `json:"authority_sha256"`
}

// SourceManagementFromDatabase independently checks the actor in the source's
// tenant. Execution-tenant changes and system-administrator status confer no grant.
func SourceManagementFromDatabase(
	ctx context.Context, db *gorm.DB, source, actor string,
) (SourceManagementAuthority, error) {
	var out SourceManagementAuthority
	if actor == "" || types.IsSyntheticUserID(actor) {
		return out, ErrRecoveryRejected
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			TenantID                                        uint64
			KBID, SourceType, CreatorID, PrivateOwner, Role string
			SourceStatus, AccessState                       string
		}
		result := tx.Raw(`SELECT d.tenant_id, d.knowledge_base_id AS kb_id, d.type AS source_type,
 k.creator_id, coalesce(p.owner_id,'') AS private_owner, m.role,
 d.status AS source_status,d.legacy_access_state AS access_state
 FROM data_sources d JOIN knowledge_bases k ON k.id=d.knowledge_base_id AND k.tenant_id=d.tenant_id
 JOIN tenants t ON t.id=d.tenant_id AND t.status='active' AND t.deleted_at IS NULL
 JOIN tenant_members m ON m.tenant_id=d.tenant_id AND m.user_id=?
 JOIN users u ON u.id=m.user_id AND u.is_active=true AND u.deleted_at IS NULL
 LEFT JOIN feishu_private_kbs p ON p.kb_id=k.id
 WHERE d.id=? AND d.deleted_at IS NULL AND k.deleted_at IS NULL
 AND m.status='active' AND m.deleted_at IS NULL`, actor, source).Scan(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || row.TenantID == 0 || (row.PrivateOwner != "" && row.PrivateOwner != actor) {
			return ErrRecoveryRejected
		}
		if row.Role != "owner" && row.Role != "admin" {
			var ds types.DataSource
			if err := tx.Where("id=?", source).First(&ds).Error; err != nil {
				return err
			}
			cfg, err := ds.ParseConfig()
			if err != nil {
				return err
			}
			ref, err := types.ParseFeishuSourceReference(cfg)
			if err != nil {
				return err
			}
			owned := row.CreatorID == actor || (ref != nil && ref.OwnerUserID == actor)
			if row.Role != "contributor" || !owned {
				return ErrRecoveryRejected
			}
		}
		// A transient status=error is not a new authorization. Its eligibility
		// is checked separately by the existing RetryEligible predicate.
		status := row.SourceStatus
		row.SourceStatus = ""
		hash, err := RecoveryDigest([]any{source, actor, row})
		if err != nil {
			return err
		}
		out = SourceManagementAuthority{
			TenantID: row.TenantID, KBID: row.KBID, SourceID: source,
			SourceType: row.SourceType, ActorID: actor, AuthorityHash: hash,
			SourceStatus: status, AccessState: row.AccessState,
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return out, err
}
