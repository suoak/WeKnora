package repository

import (
	"context"
	"errors"
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

func bindingTestDB(t *testing.T) (*userRepository, *gorm.DB, types.User) {
	r, db := sourceIDTestDB(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/003026_sourceid_binding_review.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	user := types.User{ID: uuid.NewString(), Username: "synthetic-legacy", Email: "Employee@Example.Test", IsActive: true, IsSystemAdmin: true, CanAccessAllTenants: true, TenantID: 42}
	require.NoError(t, db.Create(&user).Error)
	return r, db, user
}

func TestSourceIDEmailRJGHReviewLifecycle(t *testing.T) {
	r, db, user := bindingTestDB(t)
	ctx := context.Background()
	bound, err := r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007", " employee@EXAMPLE.TEST ")
	require.Nil(t, bound)
	require.ErrorIs(t, err, ErrSourceIDBindingPending)
	for _, model := range []interface{}{&types.ExternalIdentity{}, &types.SourceIDEnrollment{}, &types.SourceIDSession{}, &types.AuthToken{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
	rows, err := r.ListSourceIDBindingReviews(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	id := rows[0].ID
	require.Error(t, r.ReviewSourceIDBinding(ctx, id, "", "", "approve"))
	require.NoError(t, r.ReviewSourceIDBinding(ctx, id, "synthetic-admin", "verified-ownership-ticket", "approve"))
	bound, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007", "changed@example.test")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.Equal(t, user.TenantID, bound.TenantID)
	require.True(t, bound.IsSystemAdmin)
	require.True(t, bound.CanAccessAllTenants)
	require.NoError(t, db.Model(&user).Update("email", "new@example.test").Error)
	bound, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007", "")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	_, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "different-sub", "0007", "new@example.test")
	require.Error(t, err)
	_, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "different-rjgh", "new@example.test")
	require.Error(t, err)
	session := &types.SourceIDSession{ID: uuid.NewString(), Issuer: "mock-issuer", UserID: user.ID, SessionIndexHash: "synthetic-index", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, r.CreateSourceIDReviewedSession(ctx, session))
	require.NoError(t, r.ReviewSourceIDBinding(ctx, id, "synthetic-admin", "revoke-ticket", "revoke"))
	require.Error(t, r.CheckSourceIDSession(ctx, session.ID, user.ID))
	session.ID = uuid.NewString()
	require.Error(t, r.CreateSourceIDReviewedSession(ctx, session))
	_, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007", "new@example.test")
	require.Error(t, err)
	require.Error(t, r.ReviewSourceIDBinding(ctx, id, "synthetic-admin", "ticket", "approve"))
	require.NoError(t, r.ReviewSourceIDBinding(ctx, id, "synthetic-admin", "re-review-ticket", "reopen"))
	// Explicit re-review renews the snapshot for the same original user only.
	require.NoError(t, r.ReviewSourceIDBinding(ctx, id, "synthetic-admin", "renewed-ownership-ticket", "approve"))
	bound, err = r.ResolveSourceIDEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007", "ignored@example.test")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSourceIDEmailRJGHRejectsAmbiguityInactiveAndStaleCandidate(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "inactive", "changed_before_approval"} {
		t.Run(scenario, func(t *testing.T) {
			r, db, user := bindingTestDB(t)
			ctx := context.Background()
			email := "employee@example.test"
			switch scenario {
			case "missing":
				email = "missing@example.test"
			case "duplicate":
				require.NoError(t, db.Create(&types.User{ID: uuid.NewString(), Username: "duplicate", Email: "employee@example.test", IsActive: true}).Error)
			case "inactive":
				require.NoError(t, db.Model(&user).Update("is_active", false).Error)
			}
			_, err := r.ResolveSourceIDEmailIdentity(ctx, "mock", "sub", "rjgh", email)
			require.Error(t, err)
			if scenario == "changed_before_approval" {
				require.ErrorIs(t, err, ErrSourceIDBindingPending)
				rows, err := r.ListSourceIDBindingReviews(ctx)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.NoError(t, db.Model(&user).Update("email", "changed@example.test").Error)
				require.Error(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "ticket", "approve"))
			} else {
				require.False(t, errors.Is(err, ErrSourceIDBindingPending))
			}
			var count int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestSourceIDEmailRJGHConcurrentReview(t *testing.T) {
	r, db, _ := bindingTestDB(t)
	ctx := context.Background()
	_, err := r.ResolveSourceIDEmailIdentity(ctx, "mock", "sub", "rjgh", "employee@example.test")
	require.ErrorIs(t, err, ErrSourceIDBindingPending)
	rows, err := r.ListSourceIDBindingReviews(ctx)
	require.NoError(t, err)
	var success atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "synthetic-ticket", "approve") == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	// SQLite lock contention may reject every concurrent attempt safely. A
	// sequential retry is an administrator action, not automatic login retry.
	if success.Load() == 0 {
		require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "ticket", "approve"))
		success.Add(1)
	}
	require.EqualValues(t, 1, success.Load())
	for _, model := range []interface{}{&types.ExternalIdentity{}, &types.SourceIDEnrollment{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
}
