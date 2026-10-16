package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func enterpriseTestDB(t *testing.T, postgres bool) (*userRepository, *gorm.DB, types.User) {
	t.Helper()
	var r *userRepository
	var db *gorm.DB
	var user types.User
	dialect := "sqlite"
	if postgres {
		db = sourceIDPostgresDB(t)
		r = &userRepository{db: db}
		dialect = "versioned"
		for _, name := range []string{"003026_sourceid_binding_review", "003027_sourceid_local_session"} {
			data, e := os.ReadFile("../../../migrations/versioned/" + name + ".up.sql")
			require.NoError(t, e)
			require.NoError(t, db.Exec(string(data)).Error)
		}
		user = types.User{ID: uuid.NewString(), Email: "employee@example.test", Username: "synthetic", IsActive: true, TenantID: 42, IsSystemAdmin: true, CanAccessAllTenants: true}
		require.NoError(t, db.Create(&user).Error)
	} else {
		r, db, user = bindingTestDB(t)
	}
	data, e := os.ReadFile("../../../migrations/" + dialect + "/003028_sourceid_enterprise_ownership.up.sql")
	require.NoError(t, e)
	require.NoError(t, db.Exec(string(data)).Error)
	return r, db, user
}
func proof(user types.User) types.SourceIDAccountOwnership {
	return types.SourceIDAccountOwnership{Issuer: "synthetic-issuer", EmployeeNo: "synthetic-employee", UserID: user.ID, Email: strings.ToLower(user.Email), Status: "active", Revision: 1, PolicyRef: "synthetic-enterprise", SourceRef: "synthetic-directory", OwnershipRef: "synthetic-independent-history", VerifiedAt: time.Now().Add(-time.Second).UTC(), ValidUntil: time.Now().Add(time.Hour).UTC()}
}
func TestSourceIDEnterpriseOwnershipLifecycle(t *testing.T) {
	r, db, user := enterpriseTestDB(t, false)
	ctx := context.Background()
	row := proof(user)
	changed, err := r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", false)
	require.NoError(t, err)
	require.True(t, changed)
	var n int64
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Count(&n).Error)
	require.Zero(t, n)
	changed, err = r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	require.False(t, changed)
	get := func(email, sub string) (*types.User, error) {
		return r.ResolveSourceIDEnterpriseEmailIdentity(ctx, row.Issuer, sub, row.EmployeeNo, email, "synthetic-binding", row.PolicyRef, time.Hour)
	}
	bound, err := get(row.Email, "synthetic-sub")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.Equal(t, user.IsSystemAdmin, bound.IsSystemAdmin)
	require.Equal(t, user.CanAccessAllTenants, bound.CanAccessAllTenants)
	bound, err = get("changed@example.test", "synthetic-sub")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	_, err = get(row.Email, "changed-sub")
	require.Error(t, err)
	row.Revision++
	row.Email = "new@example.test"
	changed, err = r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	require.True(t, changed)
	_, err = get("new@example.test", "synthetic-sub")
	require.NoError(t, err)
	row.Revision++
	row.Status = "disabled"
	_, err = r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	_, err = get(row.Email, "synthetic-sub")
	require.Error(t, err)
	row.Status = "active"
	row.Revision++
	_, err = r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	var review types.SourceIDBindingReview
	require.NoError(t, db.First(&review).Error)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, review.ID, "synthetic-operator", "synthetic-revoke", "revoke"))
	_, err = get(row.Email, "synthetic-sub")
	require.Error(t, err)
	row.ValidUntil = time.Now().Add(-time.Second)
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Where("issuer=?", row.Issuer).Update("valid_until", row.ValidUntil).Error)
	require.Error(t, r.CheckSourceIDEnterpriseOwnership(ctx, row.Issuer, row.EmployeeNo, user.ID, row.PolicyRef, time.Hour))
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	require.Equal(t, "{}", string(raw))
}
func TestSourceIDEnterpriseOwnershipScale(t *testing.T) {
	for _, population := range []int{1000, 10000} {
		t.Run(fmt.Sprint(population), func(t *testing.T) {
			r, db, _ := enterpriseTestDB(t, false)
			ctx := context.Background()
			users := make([]types.User, population)
			for i := range users {
				users[i] = types.User{ID: uuid.NewString(), Email: fmt.Sprintf("scale-%d@example.test", i), Username: fmt.Sprintf("synthetic-scale-%d", i), IsActive: true, TenantID: 42}
			}
			require.NoError(t, db.CreateInBatches(users, 250).Error)
			start := time.Now()
			for i, u := range users {
				row := proof(u)
				row.EmployeeNo = fmt.Sprintf("synthetic-%d", i)
				_, err := r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
				require.NoError(t, err)
			}
			imported := time.Since(start)
			var n int64
			require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Count(&n).Error)
			require.EqualValues(t, population, n)
			start = time.Now()
			for i := 0; i < 1000; i++ {
				index := i % population
				require.NoError(t, r.CheckSourceIDEnterpriseOwnership(ctx, "synthetic-issuer", fmt.Sprintf("synthetic-%d", index), users[index].ID, "synthetic-enterprise", time.Hour))
			}
			t.Logf("synthetic_population=%d import_ms=%d indexed_1000_checks_ms=%d", population, imported.Milliseconds(), time.Since(start).Milliseconds())
			var plan []struct{ Detail string }
			require.NoError(t, db.Raw("EXPLAIN QUERY PLAN SELECT * FROM source_id_account_ownership WHERE issuer=? AND employee_no=?", "synthetic-issuer", "synthetic-1").Scan(&plan).Error)
			require.NotEmpty(t, plan)
			require.Contains(t, plan[0].Detail, "INDEX")
			require.NoError(t, db.Raw("EXPLAIN QUERY PLAN SELECT id FROM users WHERE LOWER(TRIM(email))=?", "scale-1@example.test").Scan(&plan).Error)
			require.Contains(t, plan[0].Detail, "idx_sourceid_users_normalized_email")
		})
	}
}
func TestSourceIDPostgresEnterpriseConcurrentOwnershipAndBinding(t *testing.T) {
	r, db, user := enterpriseTestDB(t, true)
	ctx := context.Background()
	row := proof(user)
	_, err := r.SyncSourceIDAccountOwnership(ctx, row, "synthetic-operator", true)
	require.NoError(t, err)
	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, e := r.ResolveSourceIDEnterpriseEmailIdentity(ctx, row.Issuer, "synthetic-sub", row.EmployeeNo, row.Email, "synthetic-binding", row.PolicyRef, time.Hour)
			if e == nil && u.ID == user.ID {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 12, ok.Load())
	for _, table := range []interface{}{&types.ExternalIdentity{}, &types.SourceIDEnrollment{}, &types.SourceIDBindingReview{}} {
		var n int64
		require.NoError(t, db.Model(table).Count(&n).Error)
		require.EqualValues(t, 1, n)
	}
	var after types.User
	require.NoError(t, db.First(&after, "id=?", user.ID).Error)
	require.Equal(t, user.IsSystemAdmin, after.IsSystemAdmin)
	require.Equal(t, user.CanAccessAllTenants, after.CanAccessAllTenants)
	require.Equal(t, user.TenantID, after.TenantID)
}
