package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// VerifyPortalMCPProtocolCredential reloads only credential and workspace
// eligibility. This returns no KB grant and never reads document/chunk data.
func (r *EnterpriseAccessRepository) VerifyPortalMCPProtocolCredential(
	ctx context.Context, user string, keyID, tenant uint64,
) error {
	denied := errors.New("personal MCP protocol credential denied")
	if r == nil || r.db == nil || user == "" || keyID == 0 || tenant == 0 {
		return denied
	}
	var row struct {
		types.EnterpriseCredential
		CapabilitiesJSON string `gorm:"column:capabilities"`
	}
	if err := r.db.WithContext(ctx).Table("tenant_api_keys").
		Select("id, owner_user_id, scope_type, capabilities, revoked_at, expires_at, full_access").
		Where("id=?", keyID).Take(&row).Error; err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(row.CapabilitiesJSON), &row.Capabilities); err != nil {
		return err
	}
	caps := types.TenantAPIKeyScope{Capabilities: row.Capabilities}
	if row.ID != keyID || row.OwnerUserID == nil || *row.OwnerUserID != user ||
		row.ScopeType != types.APIKeyScopeUserMCP || row.FullAccess || row.RevokedAt != nil ||
		row.ExpiresAt != nil && !time.Now().Before(*row.ExpiresAt) ||
		!caps.HasCapability(types.APIKeyCapabilityRetrieve) {
		return denied
	}
	var member struct{ Role types.TenantRole }
	if err := r.db.WithContext(ctx).Table("tenant_members m").Select("m.role").
		Joins("JOIN users u ON u.id=m.user_id AND u.is_active=true AND u.deleted_at IS NULL").
		Joins("JOIN tenants t ON t.id=m.tenant_id AND t.status='active' AND t.deleted_at IS NULL").
		Where("m.user_id=? AND m.tenant_id=? AND m.status='active' AND m.deleted_at IS NULL", user, tenant).
		Take(&member).Error; err != nil {
		return err
	}
	if !member.Role.IsValid() {
		return denied
	}
	scope, err := (&tenantAPIKeyRepository{db: r.db}).GetUserMCPTenantScope(ctx, keyID, tenant)
	if err != nil {
		return err
	}
	if scope == nil || scope.APIKeyID != keyID || scope.TenantID != tenant ||
		(scope.KBScopeMode != types.APIKeyKBScopeAll && scope.KBScopeMode != types.APIKeyKBScopeSelected) {
		return denied
	}
	return nil
}
