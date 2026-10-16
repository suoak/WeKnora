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
	for _, scenario := range []string{"inactive", "missing", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			r, db, user := bindingTestDB(t)
			syntheticOrdinaryRoles(t, db, user)
			email := "employee@example.test"
			switch scenario {
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

func TestSourceIDAutoEmailBindingAllRolesPreserveOriginalAccount(t *testing.T) {
	for _, role := range []string{"viewer", "contributor", "owner", "tenant_admin", "org_admin", "system_admin", "cross_tenant_admin", "legacy_no_membership"} {
		t.Run(role, func(t *testing.T) {
			r, db, user := bindingTestDB(t)
			syntheticOrdinaryRoles(t, db, user)
			switch role {
			case "viewer", "contributor", "owner":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role=?", role).Error)
			case "tenant_admin":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='admin'").Error)
			case "org_admin":
				require.NoError(t, db.Exec("INSERT INTO organization_tenant_members(tenant_id,role,representative_user_id) VALUES (?,?,?)", user.TenantID, "admin", user.ID).Error)
			case "system_admin":
				require.NoError(t, db.Model(&user).Update("is_system_admin", true).Error)
			case "cross_tenant_admin":
				require.NoError(t, db.Model(&user).Update("can_access_all_tenants", true).Error)
			case "legacy_no_membership":
				require.NoError(t, db.Exec("DELETE FROM tenant_members").Error)
			}
			require.NoError(t, db.Exec("CREATE TABLE synthetic_kb_acl (user_id TEXT, tenant_id BIGINT, knowledge_base_id TEXT, permission TEXT)").Error)
			require.NoError(t, db.Exec("INSERT INTO synthetic_kb_acl VALUES (?,?,'synthetic-kb','read')", user.ID, user.TenantID).Error)
			var before types.User
			require.NoError(t, db.First(&before, "id=?", user.ID).Error)
			snapshot := func(table string) []map[string]interface{} {
				var rows []map[string]interface{}
				require.NoError(t, db.Table(table).Find(&rows).Error)
				return rows
			}
			tables := []string{"tenant_members", "organization_tenant_members", "synthetic_kb_acl"}
			original := make(map[string][]map[string]interface{})
			for _, table := range tables {
				original[table] = snapshot(table)
			}
			ctx := context.Background()
			for _, email := range []string{" Employee@Example.Test ", "changed@example.test"} {
				bound, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", email, "synthetic-all-roles-policy")
				require.NoError(t, err)
				require.Equal(t, before.ID, bound.ID)
				require.Equal(t, before.TenantID, bound.TenantID)
				require.Equal(t, before.IsSystemAdmin, bound.IsSystemAdmin)
				require.Equal(t, before.CanAccessAllTenants, bound.CanAccessAllTenants)
			}
			var after types.User
			require.NoError(t, db.First(&after, "id=?", user.ID).Error)
			require.Equal(t, before, after)
			for _, table := range tables {
				require.Equal(t, original[table], snapshot(table))
			}
			var count int64
			require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			rows, err := r.ListSourceIDBindingReviews(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "synthetic-reviewer", "synthetic-revoke", "revoke"))
			_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "synthetic-all-roles-policy")
			require.Error(t, err)
		})
	}
}

func TestSourceIDAutoEmailBindingRejectsReassignedIdentity(t *testing.T) {
	for _, mismatch := range []string{"employee", "subject", "account"} {
		t.Run(mismatch, func(t *testing.T) {
			r, db, user := bindingTestDB(t)
			ctx := context.Background()
			_, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "synthetic-policy")
			require.NoError(t, err)
			sub, employee, email := "sub", "0007", "employee@example.test"
			switch mismatch {
			case "employee":
				employee = "new-employee"
			case "subject":
				sub = "new-subject"
			case "account":
				require.NoError(t, db.Model(&user).Update("email", "moved@example.test").Error)
				require.NoError(t, db.Create(&types.User{ID: uuid.NewString(), Username: "replacement", Email: email, IsActive: true}).Error)
				// A new employee cannot inherit the already bound account through
				// the old account's new email or the replacement account.
				sub, employee, email = "new-subject", "new-employee", "moved@example.test"
			}
			_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", sub, employee, email, "synthetic-policy")
			require.Error(t, err)
			var identities int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&identities).Error)
			require.EqualValues(t, 1, identities)
		})
	}
}

func TestSourceIDPostgresAutoEmailBindingConcurrentFirstLogins(t *testing.T) {
	runSourceIDPostgresAutoBindingRace(t, false)
}

func TestSourceIDPostgresPilotAutoEmailBindingConcurrentFirstLogins(t *testing.T) {
	runSourceIDPostgresAutoBindingRace(t, true)
}

func runSourceIDPostgresAutoBindingRace(t *testing.T, pilot bool) {
	db := sourceIDPostgresDB(t)
	up, err := os.ReadFile("../../../migrations/versioned/003026_sourceid_binding_review.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "synthetic-existing-tenant"}).Error)
	user := types.User{ID: uuid.NewString(), Username: "ordinary", Email: "employee@example.test", TenantID: 42, IsActive: true}
	require.NoError(t, db.Create(&user).Error)
	syntheticOrdinaryRoles(t, db, user)
	// Exercise concurrent first approval for an existing privileged account too.
	require.NoError(t, db.Model(&user).Updates(map[string]interface{}{"is_system_admin": true, "can_access_all_tenants": true}).Error)
	require.NoError(t, db.Exec("UPDATE tenant_members SET role='owner'").Error)
	r := &userRepository{db: db}
	ctx := context.Background()
	var success atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var u *types.User
			var err error
			if pilot {
				u, err = r.ResolveSourceIDPilotEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "approved-policy-v1", map[string]string{user.ID: user.Email})
			} else {
				u, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "mock", "sub", "0007", "employee@example.test", "approved-policy-v1")
			}
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
