package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func usernameJITService(t *testing.T) (*userService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "username.db")), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Tenant{}, &types.TenantMember{}))
	return &userService{config: &config.Config{}, userRepo: apprepo.NewUserRepository(db)}, db
}

func TestSourceIDJITUsernameUsesCompanyEmailPrefix(t *testing.T) {
	s, db := usernameJITService(t)
	u, err := s.sourceIDJITProvisioner("v-caiwen@example.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	require.Equal(t, "v-caiwen", u.Username)
	require.Equal(t, "!sourceid-jit-no-local-password", u.PasswordHash)
	require.True(t, *u.Preferences.OidcOnlyLogin)
	require.Zero(t, u.TenantID)
	require.False(t, u.IsSystemAdmin)
	require.False(t, u.CanAccessAllTenants)
	var n int64
	require.NoError(t, db.Model(&types.TenantMember{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestSourceIDJITUsernameCollisionDoesNotReuseAccount(t *testing.T) {
	s, db := usernameJITService(t)
	original, err := s.sourceIDJITProvisioner("employee@example.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	other, err := s.sourceIDJITProvisioner("employee@other.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	require.Equal(t, "employee", original.Username)
	require.Equal(t, "employee-2", other.Username)
	require.NotEqual(t, original.ID, other.ID)
	var persisted types.User
	require.NoError(t, db.First(&persisted, "id=?", original.ID).Error)
	require.Equal(t, original.Email, persisted.Email)
	require.Equal(t, original.Username, persisted.Username)
	// Provisioning is not the identity resolver: a duplicate email must fail closed.
	_, err = s.sourceIDJITProvisioner("employee@example.test")(context.Background(), s.userRepo)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 2, n)
}

type usernameLookupFailure struct{ interfaces.UserRepository }

func (r usernameLookupFailure) GetUserByUsername(context.Context, string) (*types.User, error) {
	return nil, errors.New("synthetic lookup unavailable")
}

func TestSourceIDJITUsernameLookupFailureCreatesNoAccount(t *testing.T) {
	s, db := usernameJITService(t)
	_, err := s.sourceIDJITProvisioner("employee@example.test")(context.Background(), usernameLookupFailure{s.userRepo})
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestSourceIDJITUsernameInvalidPrefixCreatesNoAccount(t *testing.T) {
	for _, email := range []string{"missing-at", "@example.test", "!!!@example.test", "employee@"} {
		t.Run(email, func(t *testing.T) {
			s, db := usernameJITService(t)
			_, err := s.sourceIDJITProvisioner(email)(context.Background(), s.userRepo)
			require.Error(t, err)
			var n int64
			require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
			require.Zero(t, n)
		})
	}
}

func TestSourceIDJITUsernameRetiredNameFailsClosed(t *testing.T) {
	s, db := usernameJITService(t)
	u, err := s.sourceIDJITProvisioner("employee@example.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	require.NoError(t, db.Delete(u).Error)
	_, err = s.sourceIDJITProvisioner("employee@other.test")(context.Background(), s.userRepo)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Unscoped().Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

type usernameAlwaysTaken struct{ interfaces.UserRepository }

func (r usernameAlwaysTaken) GetUserByUsername(context.Context, string) (*types.User, error) {
	return &types.User{ID: "synthetic-existing"}, nil
}
func TestSourceIDJITUsernameExhaustedCreatesNoAccount(t *testing.T) {
	s, db := usernameJITService(t)
	_, err := s.sourceIDJITProvisioner("employee@example.test")(context.Background(), usernameAlwaysTaken{s.userRepo})
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestSourceIDJITUsernameNormalizesPrefixAndBoundsLength(t *testing.T) {
	for _, tc := range []struct{ email, want string }{
		{"V-Caiwen@example.test", "v-caiwen"},
		{"first.last@example.test", "first.last"},
		{"user_name@example.test", "user_name"},
		{strings.Repeat("a", 80) + "@example.test", strings.Repeat("a", 50)},
	} {
		t.Run(tc.want, func(t *testing.T) {
			s, _ := usernameJITService(t)
			u, err := s.sourceIDJITProvisioner(tc.email)(context.Background(), s.userRepo)
			require.NoError(t, err)
			require.Equal(t, tc.want, u.Username)
		})
	}
}

func TestSourceIDJITUsernameCollisionKeepsLengthBound(t *testing.T) {
	s, _ := usernameJITService(t)
	prefix := strings.Repeat("a", 50)
	first, err := s.sourceIDJITProvisioner(prefix+"@example.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	second, err := s.sourceIDJITProvisioner(prefix+"@other.test")(context.Background(), s.userRepo)
	require.NoError(t, err)
	require.Equal(t, prefix, first.Username)
	require.Equal(t, strings.Repeat("a", 48)+"-2", second.Username)
	require.NotEqual(t, first.ID, second.ID)
}
