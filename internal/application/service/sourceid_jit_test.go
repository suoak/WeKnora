package service

import (
	"context"
	"path/filepath"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSourceIDJITExternalOnlyRegistrationKeepsPasswordRegistration(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "jit.db")),
		&gorm.Config{Logger: logger.Discard},
	)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Tenant{}, &types.TenantMember{}))
	s := &userService{config: &config.Config{}, userRepo: apprepo.NewUserRepository(db)}
	request := func(email string) *types.RegisterRequest {
		return &types.RegisterRequest{
			Username:           email,
			Email:              email,
			Password:           "synthetic-only-password",
			TenantProvisioning: types.TenantProvisioningTenantless,
		}
	}
	normal, err := s.Register(context.Background(), request("password@example.test"))
	require.NoError(t, err)
	require.NoError(
		t,
		bcrypt.CompareHashAndPassword([]byte(normal.PasswordHash), []byte("synthetic-only-password")),
	)
	externalCtx := context.WithValue(context.Background(), sourceIDJITExternalOnlyKey{}, true)
	external, err := s.Register(externalCtx, request("external@example.test"))
	require.NoError(t, err)
	require.Equal(t, "!sourceid-jit-no-local-password", external.PasswordHash)
	require.Error(
		t,
		bcrypt.CompareHashAndPassword([]byte(external.PasswordHash), []byte("synthetic-only-password")),
	)
	require.Zero(t, external.TenantID)
	require.False(t, external.IsSystemAdmin)
	require.False(t, external.CanAccessAllTenants)
	invalid := request("invalid@example.test")
	invalid.TenantProvisioning = types.TenantProvisioningCreatePersonal
	_, err = s.Register(externalCtx, invalid)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.Tenant{}).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Model(&types.TenantMember{}).Count(&n).Error)
	require.Zero(t, n)
}
