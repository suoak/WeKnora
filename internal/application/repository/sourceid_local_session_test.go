package repository

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sourceIDLocalRepositoryFixture(t *testing.T, db *gorm.DB, dialect string) (*userRepository, *types.SourceIDSession, *types.AuthToken) {
	t.Helper()
	for _, migration := range []string{"003026_sourceid_binding_review", "003027_sourceid_local_session"} {
		sql, err := os.ReadFile("../../../migrations/" + dialect + "/" + migration + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(sql)).Error)
	}
	r := &userRepository{db: db}
	require.NoError(t, db.AutoMigrate(&types.Tenant{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "synthetic-local-space"}).Error)
	user := types.User{ID: uuid.NewString(), Email: "synthetic-local@example.test", Username: "synthetic-local", IsActive: true, TenantID: 42}
	require.NoError(t, db.Create(&user).Error)
	identity := types.ExternalIdentity{ID: uuid.NewString(), Provider: "sourceid", Issuer: "mock", Subject: "synthetic-sub", EmployeeNo: "synthetic-employee", UserID: user.ID, CreatedAt: time.Now(), UpdatedAt: time.Now(), LastLoginAt: time.Now()}
	require.NoError(t, db.Create(&identity).Error)
	require.NoError(t, db.Create(&types.SourceIDBindingReview{ID: uuid.NewString(), Issuer: "mock", EmployeeNo: identity.EmployeeNo, Subject: identity.Subject, UserID: user.ID, Email: user.Email, Status: "approved", ReviewedBy: "synthetic-policy", ReviewReference: "synthetic-approval", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
	session := &types.SourceIDSession{ID: uuid.NewString(), Issuer: "mock", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}
	local := &types.SourceIDLocalSession{ID: session.ID, Subject: identity.Subject, PolicyRef: "synthetic-approval", AdmissionPolicyRef: "synthetic-admission", AdmissionValidUntil: session.ExpiresAt, LastSeenAt: time.Now(), IdleSeconds: 1800, AccessSeconds: 600, RefreshEnabled: true}
	local.AdmissionSource = "sid_query"
	local.AdmissionCheckedAt = time.Now()
	require.NoError(t, r.CreateSourceIDLocalSession(context.Background(), session, local))
	access := &types.AuthToken{ID: uuid.NewString(), UserID: user.ID, SessionID: session.ID, Token: "synthetic-access", TokenType: "access_token", ExpiresAt: time.Now().Add(10 * time.Minute), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	refresh := &types.AuthToken{ID: uuid.NewString(), UserID: user.ID, SessionID: session.ID, Token: "synthetic-refresh", TokenType: "refresh_token", ExpiresAt: session.ExpiresAt, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, r.PersistSourceIDTokens(context.Background(), session.ID, access, refresh, ""))
	return r, session, refresh
}

func TestSourceIDLocalMigrationRollbackRevokesSessions(t *testing.T) {
	_, db := sourceIDTestDB(t)
	r, session, refresh := sourceIDLocalRepositoryFixture(t, db, "sqlite")
	ctx := context.Background()
	require.NoError(t, r.CheckSourceIDSession(ctx, session.ID, session.UserID))
	sql, err := os.ReadFile("../../../migrations/sqlite/003027_sourceid_local_session.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	require.Error(t, r.CheckSourceIDSession(ctx, session.ID, session.UserID))
	var row types.AuthToken
	require.NoError(t, db.First(&row, "id = ?", refresh.ID).Error)
	require.True(t, row.IsRevoked)
	var users int64
	require.NoError(t, db.Model(&types.User{}).Count(&users).Error)
	require.EqualValues(t, 1, users)
}

func TestSourceIDPostgresLocalRefreshRaceAndReplay(t *testing.T) {
	db := sourceIDPostgresDB(t)
	r, session, previous := sourceIDLocalRepositoryFixture(t, db, "versioned")
	var wg sync.WaitGroup
	var success atomic.Int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			access := &types.AuthToken{ID: uuid.NewString(), UserID: session.UserID, SessionID: session.ID, Token: "synthetic-" + uuid.NewString(), TokenType: "access_token", ExpiresAt: time.Now().Add(10 * time.Minute), CreatedAt: time.Now(), UpdatedAt: time.Now()}
			refresh := &types.AuthToken{ID: uuid.NewString(), UserID: session.UserID, SessionID: session.ID, Token: "synthetic-" + uuid.NewString(), TokenType: "refresh_token", ExpiresAt: session.ExpiresAt, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if r.PersistSourceIDTokens(context.Background(), session.ID, access, refresh, previous.Token) == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.Error(t, r.CheckSourceIDSession(context.Background(), session.ID, session.UserID))
}
