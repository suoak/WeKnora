package repository

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSourceIDPostgresEmailBindingApprovalAndMigrationRollback(t *testing.T) {
	db := sourceIDPostgresDB(t)
	up, err := os.ReadFile("../../../migrations/versioned/003026_sourceid_binding_review.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	r := &userRepository{db: db}
	ctx := context.Background()
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "synthetic-existing-tenant"}).Error)
	user := types.User{ID: uuid.NewString(), Username: "synthetic-pg", Email: "Pg@Example.Test", IsActive: true, TenantID: 42}
	require.NoError(t, db.Create(&user).Error)
	_, err = r.ResolveSourceIDEmailIdentity(ctx, "mock", "sub", "0007", "pg@example.test")
	require.ErrorIs(t, err, ErrSourceIDBindingPending)
	rows, err := r.ListSourceIDBindingReviews(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	var success atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "ticket", "approve") == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	bound, err := r.ResolveSourceIDEmailIdentity(ctx, "mock", "sub", "0007", "changed@example.test")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.Equal(t, user.TenantID, bound.TenantID)
	var count int64
	require.NoError(t, db.Model(&types.SourceIDBindingAudit{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "revoke-ticket", "revoke"))
	_, err = r.ResolveSourceIDEmailIdentity(ctx, "mock", "sub", "0007", "pg@example.test")
	require.Error(t, err)
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	down, err := os.ReadFile("../../../migrations/versioned/003026_sourceid_binding_review.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
