package repository

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func syntheticOrdinaryRoles(t *testing.T, db *gorm.DB, user types.User) {
	t.Helper()
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS tenant_members (user_id TEXT, tenant_id BIGINT, role TEXT, status TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS organization_tenant_members (tenant_id BIGINT, role TEXT, representative_user_id TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members (user_id,tenant_id,role,status) VALUES (?,?,?,?)", user.ID, user.TenantID, "contributor", "active").Error)
	require.NoError(t, db.Model(&user).Updates(map[string]interface{}{"is_system_admin": false, "can_access_all_tenants": false}).Error)
}

func TestSourceIDAutoEmailBindingSyntheticLifecycle(t *testing.T) {
	r, db, user := bindingTestDB(t)
	syntheticOrdinaryRoles(t, db, user)
	ctx := context.Background()
	_, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "")
	require.ErrorIs(t, err, ErrSourceIDBindingPending)
	bound, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", " employee@EXAMPLE.TEST ", "approved-policy-v1")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.Equal(t, user.TenantID, bound.TenantID)
	require.NoError(t, db.Model(&user).Update("email", "changed@example.test").Error)
	bound, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "unrelated@example.test", "approved-policy-v1")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "new-sub", "0007", "changed@example.test", "approved-policy-v1")
	require.Error(t, err)
	rows, err := r.ListSourceIDBindingReviews(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "enterprise-auto-policy", rows[0].ReviewedBy)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "revoke-ticket", "revoke"))
	_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "changed@example.test", "approved-policy-v1")
	require.Error(t, err)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "reopen-ticket", "reopen"))
	_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "changed@example.test", "approved-policy-v1")
	require.ErrorIs(t, err, ErrSourceIDBindingPending)
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSourceIDAutoEmailBindingRejectsExceptionalAccounts(t *testing.T) {
	for _, scenario := range []string{"system_admin", "global_access", "tenant_owner", "tenant_admin", "org_admin", "legacy_role_unknown", "unknown_role", "no_roles_table", "inactive", "missing", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			r, db, user := bindingTestDB(t)
			if scenario != "no_roles_table" {
				syntheticOrdinaryRoles(t, db, user)
			}
			email := "employee@example.test"
			switch scenario {
			case "system_admin":
				require.NoError(t, db.Model(&user).Update("is_system_admin", true).Error)
			case "global_access":
				require.NoError(t, db.Model(&user).Update("can_access_all_tenants", true).Error)
			case "tenant_owner":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='owner'").Error)
			case "tenant_admin":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='admin'").Error)
			case "org_admin":
				require.NoError(t, db.Exec("INSERT INTO organization_tenant_members(tenant_id,role,representative_user_id) VALUES (?,?,?)", user.TenantID, "admin", user.ID).Error)
			case "legacy_role_unknown":
				require.NoError(t, db.Exec("DELETE FROM tenant_members").Error)
			case "unknown_role":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='unknown'").Error)
			case "inactive":
				require.NoError(t, db.Model(&user).Update("is_active", false).Error)
			case "missing":
				email = "missing@example.test"
			case "duplicate":
				require.NoError(t, db.Create(&types.User{ID: uuid.NewString(), Username: "duplicate", Email: "employee@example.test", IsActive: true}).Error)
			}
			_, err := r.ResolveSourceIDAutoEmailIdentity(context.Background(), "mock", "sub", "0007", email, "approved-policy-v1")
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestSourceIDPostgresAutoEmailBindingConcurrentFirstLogins(t *testing.T) {
	db := sourceIDPostgresDB(t)
	up, err := os.ReadFile("../../../migrations/versioned/003026_sourceid_binding_review.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "synthetic-existing-tenant"}).Error)
	user := types.User{ID: uuid.NewString(), Username: "ordinary", Email: "employee@example.test", TenantID: 42, IsActive: true}
	require.NoError(t, db.Create(&user).Error)
	syntheticOrdinaryRoles(t, db, user)
	r := &userRepository{db: db}
	ctx := context.Background()
	var success atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "approved-policy-v1")
			if err == nil && u.ID == user.ID {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 8, success.Load())
	for _, model := range []interface{}{&types.ExternalIdentity{}, &types.SourceIDEnrollment{}, &types.SourceIDBindingReview{}, &types.SourceIDBindingAudit{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
}
