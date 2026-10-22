package service

import (
	"context"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// PortalPolicyAuthorityConfig is explicit server-side policy, not a JWT flag,
// route permission or environment-derived implicit grant. The zero value denies.
// Production startup does not enable this configuration.
type PortalPolicyAuthorityConfig struct {
	Enabled                bool
	AllowSystemAdminReview bool
	ContractRef            string
}

type PortalPolicyAuthority struct {
	DB     *gorm.DB
	Config PortalPolicyAuthorityConfig
}

func portalJWTActor(ctx context.Context) (string, error) {
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	_, key := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || p.Type != types.PrincipalWebUser || p.ID == "" || types.CallerFromContext(ctx).UserID != p.ID || key {
		return "", access.ErrForbidden
	}
	return p.ID, nil
}

func portalLiveOwner(tx *gorm.DB, user string, tenant uint64) bool {
	var count int64
	err := tx.Table("tenant_members m").Joins("JOIN users u ON u.id=m.user_id AND u.is_active=true AND u.deleted_at IS NULL").Where("m.user_id=? AND m.tenant_id=? AND m.role='owner' AND m.status='active' AND m.deleted_at IS NULL", user, tenant).Count(&count).Error
	return err == nil && count == 1
}

func portalLiveReviewer(ctx context.Context, tx *gorm.DB, user string, config PortalPolicyAuthorityConfig) bool {
	if !config.Enabled || !config.AllowSystemAdminReview || config.ContractRef == "" || !types.IsSystemAdminFromContext(ctx) {
		return false
	}
	var count int64
	err := tx.Table("users").Where("id=? AND is_active=true AND is_system_admin=true AND deleted_at IS NULL", user).Count(&count).Error
	return err == nil && count == 1
}

func (a *PortalPolicyAuthority) AuthorizeEnterprisePolicyChange(ctx context.Context, actor string, target types.EnterprisePolicyTarget, action string) error {
	uid, err := portalJWTActor(ctx)
	if err != nil || uid != actor || a == nil || a.DB == nil || !a.Config.Enabled || a.Config.ContractRef == "" {
		return access.ErrForbidden
	}
	tx := a.DB.WithContext(ctx)
	var tenant uint64
	var resource struct{ TenantID uint64 }
	switch target.Kind {
	case "space":
		tenant, err = strconv.ParseUint(target.ID, 10, 64)
	case "kb":
		err = tx.Table("knowledge_bases").Select("tenant_id").Where("id=? AND deleted_at IS NULL", target.ID).Take(&resource).Error
		tenant = resource.TenantID
	case "knowledge":
		err = tx.Table("knowledges").Select("tenant_id").Where("id=? AND deleted_at IS NULL", target.ID).Take(&resource).Error
		tenant = resource.TenantID
	default:
		return access.ErrForbidden
	}
	if err != nil || tenant == 0 {
		return access.ErrForbidden
	}
	owner := portalLiveOwner(tx, uid, tenant)
	reviewer := portalLiveReviewer(ctx, tx, uid, a.Config)
	if action == "propose" && owner || action == "approve" && reviewer || action == "revoke" && (owner || reviewer) {
		return nil
	}
	return access.ErrForbidden
}

// ConfigurePortalPublication is deliberately not called by production wiring.
// Isolated composition can enable the authorized role mapping without enabling
// public MCP or installing a synthetic employee provider in production.
func ConfigurePortalPublication(svc interface{}, config PortalPolicyAuthorityConfig) error {
	s, ok := svc.(*portalService)
	if !ok || s == nil || s.db == nil {
		return access.ErrForbidden
	}
	s.publicationConfig = config
	return nil
}

func (s *portalService) publicationWorkflow() *PortalFirstConfirmation {
	return &PortalFirstConfirmation{DB: s.db, RequireVersions: true, AuthorityFactory: func(tx *gorm.DB) access.EnterprisePolicyAuthority {
		return &PortalPolicyAuthority{DB: tx, Config: s.publicationConfig}
	}}
}
