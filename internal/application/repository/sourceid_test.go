package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func sourceIDTestDB(t *testing.T) (*userRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sid.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuthToken{}))
	// Execute the actual versioned SQLite migration, including token column.
	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
	require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
	sql, err := os.ReadFile("../../../migrations/sqlite/003024_sourceid_oidc.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	return &userRepository{db: db}, db
}

func TestSourceIDIdentityPreservesUserAndRejectsConflicts(t *testing.T) {
	r, db := sourceIDTestDB(t)
	ctx := context.Background()
	user := types.User{
		ID:                  uuid.NewString(),
		Username:            "legacy",
		Email:               "old@example.test",
		IsActive:            true,
		IsSystemAdmin:       true,
		CanAccessAllTenants: true,
	}
	require.NoError(t, db.Create(&user).Error)
	_, err := r.ResolveSourceIDIdentity(ctx, "mock-issuer", "sub", "001")
	require.Error(t, err)
	require.NoError(
		t,
		db.Create(
			&types.SourceIDEnrollment{
				ID:              uuid.NewString(),
				Issuer:          "mock-issuer",
				EmployeeNo:      "001",
				UserID:          user.ID,
				ReviewedBy:      "mock-reviewer",
				ReviewReference: "ticket-1",
				CreatedAt:       time.Now(),
			},
		).Error,
	)
	bound, err := r.ResolveSourceIDIdentity(ctx, "mock-issuer", "sub", "001")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.True(t, bound.IsSystemAdmin)
	require.True(t, bound.CanAccessAllTenants)
	require.NoError(t, db.Model(&user).Update("email", "new@example.test").Error)
	bound, err = r.ResolveSourceIDIdentity(ctx, "mock-issuer", "sub", "001")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	_, err = r.ResolveSourceIDIdentity(ctx, "mock-issuer", "other-sub", "001")
	require.Error(t, err)
	_, err = r.ResolveSourceIDIdentity(ctx, "other-issuer", "sub", "001")
	require.Error(t, err)
	_, err = r.ResolveSourceIDIdentity(ctx, "mock-issuer", "sub", "002")
	require.Error(t, err)
	require.NoError(t, db.Model(&user).Update("is_active", false).Error)
	_, err = r.ResolveSourceIDIdentity(ctx, "mock-issuer", "sub", "001")
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSourceIDSessionRevocationAndRotation(t *testing.T) {
	r, db := sourceIDTestDB(t)
	ctx := context.Background()
	userID := uuid.NewString()
	require.NoError(
		t,
		db.Create(
			&types.User{ID: userID, Username: "mock", Email: "mock@example.test", IsActive: true},
		).Error,
	)
	newSession := func(index string) *types.SourceIDSession {
		session := &types.SourceIDSession{
			ID:               uuid.NewString(),
			Issuer:           "issuer",
			SessionIndexHash: sourceIDIndexKey("issuer", index),
			UserID:           userID,
			ExpiresAt:        time.Now().Add(time.Hour),
			CreatedAt:        time.Now(),
		}
		require.NoError(t, r.CreateSourceIDSession(ctx, session))
		return session
	}
	first, second := newSession("device-1"), newSession("device-2")
	pair := func(id string) (*types.AuthToken, *types.AuthToken) {
		return &types.AuthToken{
				ID:        uuid.NewString(),
				SessionID: id,
				UserID:    userID,
				Token:     uuid.NewString(),
				TokenType: "access_token",
				ExpiresAt: time.Now().Add(time.Minute),
			}, &types.AuthToken{
				ID:        uuid.NewString(),
				SessionID: id,
				UserID:    userID,
				Token:     uuid.NewString(),
				TokenType: "refresh_token",
				ExpiresAt: time.Now().Add(time.Minute),
			}
	}
	a, b := pair(first.ID)
	require.NoError(t, r.PersistSourceIDTokens(ctx, first.ID, a, b, ""))
	c, d := pair(first.ID)
	require.NoError(t, r.PersistSourceIDTokens(ctx, first.ID, c, d, b.Token))
	e, f := pair(first.ID)
	require.Error(t, r.PersistSourceIDTokens(ctx, first.ID, e, f, b.Token))
	require.NoError(t, r.RevokeSourceIDIndex(ctx, "issuer", "unknown"))
	require.NoError(t, r.CheckSourceIDSession(ctx, first.ID, userID))
	require.NoError(t, r.RevokeSourceIDIndex(ctx, "issuer", "device-1"))
	require.NoError(t, r.RevokeSourceIDIndex(ctx, "issuer", "device-1"))
	require.Error(t, r.CheckSourceIDSession(ctx, first.ID, userID))
	require.NoError(t, r.CheckSourceIDSession(ctx, second.ID, userID))
	require.Error(t, r.PersistSourceIDTokens(ctx, first.ID, e, f, d.Token))
	// A new repository instance reads durable revocation; callback after logout
	// cannot resurrect a session with the same external index.
	restarted := &userRepository{db: db}
	require.Error(t, restarted.CheckSourceIDSession(ctx, first.ID, userID))
	later := newSession("device-1")
	require.Error(t, r.CheckSourceIDSession(ctx, later.ID, userID))
	var live int64
	require.NoError(
		t,
		db.Model(&types.AuthToken{}).
			Where("session_id = ? AND is_revoked = ?", first.ID, false).
			Count(&live).
			Error,
	)
	require.Zero(t, live)
}

func TestSourceIDOIDCTransactionAndRollback(t *testing.T) {
	r, db := sourceIDTestDB(t)
	ctx := context.Background()
	require.NoError(
		t,
		r.SaveOIDCTransaction(
			ctx,
			&types.OIDCTransaction{
				Key:       "state",
				Nonce:     "nonce",
				Verifier:  "verifier",
				ExpiresAt: time.Now().Add(time.Minute),
			},
		),
	)
	value, err := r.ConsumeOIDCTransaction(ctx, "state")
	require.NoError(t, err)
	require.Equal(t, "verifier", value.Verifier)
	_, err = r.ConsumeOIDCTransaction(ctx, "state")
	require.Error(t, err)
	require.NoError(
		t,
		r.SaveOIDCTransaction(
			ctx,
			&types.OIDCTransaction{Key: "expired", ExpiresAt: time.Now().Add(-time.Minute)},
		),
	)
	_, err = r.ConsumeOIDCTransaction(ctx, "expired")
	require.Error(t, err)
	sql, err := os.ReadFile("../../../migrations/sqlite/003024_sourceid_oidc.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	require.True(t, db.Migrator().HasTable(&types.User{}))
	require.False(t, db.Migrator().HasTable(&types.SourceIDSession{}))
}
