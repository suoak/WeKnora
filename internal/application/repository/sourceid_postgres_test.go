package repository

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSourceIDPostgresMigrationAndRevocation(t *testing.T) {
	dsn := os.Getenv("SOURCEID_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ephemeral PostgreSQL fixture not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adminSQL.Close() })
	schema := "sourceid_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuthToken{}))
	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
	require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
	up, err := os.ReadFile("../../../migrations/versioned/003024_sourceid_oidc.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	r := &userRepository{db: db}
	ctx := context.Background()
	userID := uuid.NewString()
	require.NoError(t, NewUserRepository(db).CreateUser(ctx, &types.User{ID: userID, Username: "mock", Email: "mock@example.test", IsActive: true}))
	session := &types.SourceIDSession{ID: uuid.NewString(), Issuer: "mock", SessionIndexHash: sourceIDIndexKey("mock", "index"), UserID: userID, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}
	require.NoError(t, r.CreateSourceIDSession(ctx, session))
	a := &types.AuthToken{ID: uuid.NewString(), UserID: userID, SessionID: session.ID, Token: "mock-access", TokenType: "access_token"}
	b := &types.AuthToken{ID: uuid.NewString(), UserID: userID, SessionID: session.ID, Token: "mock-refresh", TokenType: "refresh_token"}
	require.NoError(t, r.PersistSourceIDTokens(ctx, session.ID, a, b, ""))
	require.NoError(t, r.RevokeSourceIDIndex(ctx, "mock", "index"))
	require.NoError(t, r.RevokeSourceIDIndex(ctx, "mock", "index"))
	require.Error(t, r.CheckSourceIDSession(ctx, session.ID, userID))
	require.Error(t, r.PersistSourceIDTokens(ctx, session.ID, a, b, b.Token))
	down, err := os.ReadFile("../../../migrations/versioned/003024_sourceid_oidc.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
