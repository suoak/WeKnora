package repository

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestPortalM7KnowHubAccountNotEmployment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}))
	require.NoError(t, db.Create(&types.User{ID: "u", Username: "synthetic", Email: "u@example.invalid", IsActive: true}).Error)
	v := &KnowHubPersonalUserVerifier{DB: db}
	ctx := types.WithTenantAPIKeyScope(types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{UserID: "u"}), types.Principal{Type: types.PrincipalWebUser, ID: "u"}), types.TenantAPIKeyScope{KeyID: 1, ScopeType: types.APIKeyScopeUserMCP})
	ok, err := v.IsEnterpriseEmployee(ctx, "u")
	require.NoError(t, err)
	require.True(t, ok)
	for _, p := range []string{types.PrincipalMCPEndpoint, types.PrincipalAPITenant, types.PrincipalAPIPlatform, types.PrincipalEmbedVisitor} {
		ok, err := v.IsEnterpriseEmployee(types.WithPrincipal(ctx, types.Principal{Type: p, ID: "u"}), "u")
		require.Error(t, err)
		require.False(t, ok)
	}
	ok, err = v.IsEnterpriseEmployee(ctx, "other")
	require.Error(t, err)
	require.False(t, ok)
	require.NoError(t, db.Model(&types.User{}).Where("id='u'").Update("is_active", false).Error)
	ok, err = v.IsEnterpriseEmployee(ctx, "u")
	require.NoError(t, err)
	require.False(t, ok)
}
