package repository

import (
	"context"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// KnowHubPersonalUserVerifier checks existing account eligibility, not current
// employment. SID/JIT and account lifecycle remain owned by the existing login
// system. Key owner, capabilities, revocation and scopes are rechecked by the
// existing live credential and Grant components on every request.
type KnowHubPersonalUserVerifier struct{ DB *gorm.DB }

func (v *KnowHubPersonalUserVerifier) IsEnterpriseEmployee(ctx context.Context, user string) (bool, error) {
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	scope, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if v == nil || v.DB == nil || !ok || p.Type != types.PrincipalWebUser || p.ID == "" || p.ID != user || types.CallerFromContext(ctx).UserID != user || !keyed || scope.KeyID == 0 || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess {
		return false, access.ErrForbidden
	}
	var count int64
	err := v.DB.WithContext(ctx).Table("users").Where("id=? AND is_active=true AND deleted_at IS NULL", user).Count(&count).Error
	if err != nil {
		return false, access.ErrForbidden
	}
	return count == 1, nil
}
