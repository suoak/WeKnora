package accountprep

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T, pg bool) (*gorm.DB, Input) {
	t.Helper()
	var db *gorm.DB
	var err error
	if pg {
		dsn := os.Getenv("SOURCEID_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("isolated PostgreSQL not configured")
		}
		admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
		require.NoError(t, e)
		schema := "accountprep_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() {
			_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
			s, _ := admin.DB()
			_ = s.Close()
		})
		u, e := url.Parse(dsn)
		require.NoError(t, e)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		db, err = gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	} else {
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fixture.db")), &gorm.Config{Logger: logger.Discard})
	}
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if !pg {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}, &types.AuthToken{}, &types.ExternalIdentity{}, &types.SourceIDBindingReview{}, &types.SourceIDEnrollment{}, &types.SourceIDSession{}, &types.AuditLog{}))
	require.NoError(t, db.Exec("CREATE TABLE organization_tenant_members (tenant_id bigint, representative_user_id text, role text)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX prep_member_unique ON tenant_members (user_id,tenant_id) WHERE deleted_at IS NULL").Error)
	owner := types.User{ID: uuid.NewString(), Username: "synthetic-owner", Email: "owner@example.test", TenantID: 7, IsActive: true}
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "synthetic-existing", Status: "active"}).Error)
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&types.TenantMember{UserID: owner.ID, TenantID: 7, Role: "owner", Status: "active"}).Error)
	return db, Input{Email: "synthetic@example.test", OperationID: uuid.NewString(), OperatorID: owner.ID, Approval: "synthetic-preparation-approval", TenantID: 7, Role: "viewer"}
}

func count(t *testing.T, db *gorm.DB, table string, expected int64) {
	t.Helper()
	var n int64
	require.NoError(t, db.Table(table).Count(&n).Error)
	require.Equal(t, expected, n, table)
}

func lifecycle(t *testing.T, pg bool) {
	db, in := fixture(t, pg)
	ctx := context.Background()
	r, err := Run(ctx, db, in)
	require.NoError(t, err)
	require.True(t, r.DryRun && r.CreateUser && r.AddMembership)
	count(t, db, "users", 1)
	count(t, db, "audit_logs", 0)
	in.Apply = true
	r, err = Run(ctx, db, in)
	require.NoError(t, err)
	require.True(t, r.CreateUser)
	var u types.User
	require.NoError(t, db.Where("email = ?", in.Email).First(&u).Error)
	require.EqualValues(t, 7, u.TenantID)
	require.False(t, u.IsSystemAdmin || u.CanAccessAllTenants)
	require.NotEmpty(t, u.PasswordHash)
	count(t, db, "tenants", 1)
	count(t, db, "users", 2)
	count(t, db, "tenant_members", 2)
	count(t, db, "external_identities", 0)
	r, err = Run(ctx, db, in)
	require.NoError(t, err)
	require.True(t, r.AlreadyApplied)
	in.OperationID = uuid.NewString()
	r, err = Run(ctx, db, in)
	require.NoError(t, err)
	require.False(t, r.CreateUser || r.AddMembership)
	count(t, db, "users", 2)
	// Only the first operation's membership can be detached.
	var first types.AuditLog
	require.NoError(t, db.Where("action = ?", preparedAction).Order("id").First(&first).Error)
	in.OperationID, in.Cleanup = first.ScopeID, true
	in.Apply = false
	_, err = Run(ctx, db, in)
	require.NoError(t, err)
	count(t, db, "tenant_members", 2)
	in.Apply = true
	_, err = Run(ctx, db, in)
	require.NoError(t, err)
	count(t, db, "users", 2)          // No account deletion, including cleanup.
	count(t, db, "tenant_members", 2) // Raw table still contains soft-deleted row.
	uid := u.ID
	u = types.User{}
	require.NoError(t, db.First(&u, "id = ?", uid).Error)
	require.False(t, u.IsActive)
	require.Zero(t, u.TenantID)
	_, err = Run(ctx, db, in)
	require.Error(t, err) // Closed operation cannot be reused.
	var owner types.TenantMember
	require.NoError(t, db.First(&owner, "user_id = ?", in.OperatorID).Error)
	require.Equal(t, types.TenantRoleOwner, owner.Role)
}

func TestSourceIDAccountPrepLifecycle(t *testing.T)         { lifecycle(t, false) }
func TestSourceIDAccountPrepPostgresLifecycle(t *testing.T) { lifecycle(t, true) }

func TestSourceIDAccountPrepRollbackAndNoLeak(t *testing.T) {
	for _, table := range []string{"tenant_members", "audit_logs"} {
		t.Run(table, func(t *testing.T) {
			db, in := fixture(t, false)
			in.Apply = true
			// Failure after registration, or after both registration and membership.
			require.NoError(t, db.Exec(fmt.Sprintf("CREATE TRIGGER fail_prep BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'synthetic@example.test credential-like-private-data'); END", table)).Error)
			r, err := Run(context.Background(), db, in)
			require.Error(t, err)
			require.Equal(t, "preparation_rejected", err.Error())
			data, _ := json.Marshal(r)
			require.NotContains(t, string(data), in.Email)
			require.NotContains(t, err.Error(), "private")
			count(t, db, "users", 1)
			count(t, db, "tenant_members", 1)
			count(t, db, "audit_logs", 0)
		})
	}
}

func TestSourceIDAccountPrepPermissionBoundaries(t *testing.T) {
	for _, kind := range []string{"owner_role", "contributor_unapproved", "non_owner_operator", "disabled", "system_admin", "cross_tenant", "other_owner", "org_admin", "identity", "review", "enrollment", "session", "ambiguous_email", "missing_schema", "wrong_operation"} {
		t.Run(kind, func(t *testing.T) {
			db, in := fixture(t, false)
			u := types.User{ID: uuid.NewString(), Email: in.Email, Username: "synthetic-existing", IsActive: true}
			require.NoError(t, db.Create(&u).Error)
			switch kind {
			case "owner_role":
				in.Role = "owner"
			case "contributor_unapproved":
				in.Role = "contributor"
			case "non_owner_operator":
				in.OperatorID = uuid.NewString()
			case "disabled":
				require.NoError(t, db.Model(&u).Update("is_active", false).Error)
			case "system_admin":
				require.NoError(t, db.Model(&u).Update("is_system_admin", true).Error)
			case "cross_tenant":
				require.NoError(t, db.Model(&u).Update("can_access_all_tenants", true).Error)
			case "other_owner":
				require.NoError(t, db.Create(&types.TenantMember{UserID: u.ID, TenantID: 99, Role: "owner", Status: "active"}).Error)
			case "org_admin":
				require.NoError(t, db.Exec("INSERT INTO organization_tenant_members VALUES(7,?, 'admin')", u.ID).Error)
			case "identity":
				require.NoError(t, db.Create(&types.ExternalIdentity{ID: uuid.NewString(), UserID: u.ID}).Error)
			case "review":
				require.NoError(t, db.Create(&types.SourceIDBindingReview{ID: uuid.NewString(), UserID: u.ID, Status: "revoked"}).Error)
			case "enrollment":
				require.NoError(t, db.Create(&types.SourceIDEnrollment{ID: uuid.NewString(), UserID: u.ID}).Error)
			case "session":
				require.NoError(t, db.Create(&types.SourceIDSession{ID: uuid.NewString(), UserID: u.ID}).Error)
			case "ambiguous_email":
				require.NoError(t, db.Create(&types.User{ID: uuid.NewString(), Email: "SYNTHETIC@example.test", Username: "synthetic-other"}).Error)
			case "missing_schema":
				require.NoError(t, db.Migrator().DropTable("source_id_enrollments"))
			case "wrong_operation":
				in.Apply = true
				_, err := Run(context.Background(), db, in)
				require.NoError(t, err)
				in.Email = "other@example.test"
			}
			in.Apply = true
			_, err := Run(context.Background(), db, in)
			require.Error(t, err)
			expectedUsers := int64(2)
			if kind == "ambiguous_email" {
				expectedUsers = 3
			}
			count(t, db, "users", expectedUsers)
		})
	}
}

func TestSourceIDAccountPrepPostgresConcurrent(t *testing.T) {
	db, in := fixture(t, true)
	in.Apply = true
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := Run(context.Background(), db, in); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	count(t, db, "users", 2)
	count(t, db, "tenant_members", 2)
	count(t, db, "audit_logs", 1)
}

func TestSourceIDAccountPrepExistingHomeAndCleanupGuard(t *testing.T) {
	db, in := fixture(t, false)
	require.NoError(t, db.Create(&types.Tenant{ID: 8, Name: "synthetic-original", Status: "active"}).Error)
	u := types.User{ID: uuid.NewString(), Email: in.Email, Username: "synthetic-existing", TenantID: 8, IsActive: true}
	require.NoError(t, db.Create(&u).Error)
	require.NoError(t, db.Create(&types.TenantMember{UserID: u.ID, TenantID: 8, Role: "viewer", Status: "active"}).Error)
	in.Apply, in.ContributorApproved, in.Role = true, true, "contributor"
	r, err := Run(context.Background(), db, in)
	require.NoError(t, err)
	require.False(t, r.CreateUser)
	require.True(t, r.AddMembership)
	var after types.User
	require.NoError(t, db.First(&after, "id = ?", u.ID).Error)
	require.Equal(t, u.TenantID, after.TenantID)
	identity := types.ExternalIdentity{ID: uuid.NewString(), UserID: u.ID}
	require.NoError(t, db.Create(&identity).Error)
	in.Cleanup = true
	_, err = Run(context.Background(), db, in)
	require.Error(t, err) // Cannot detach an account that has since been bound.
	count(t, db, "tenant_members", 3)
	require.NoError(t, db.Delete(&identity).Error) // Synthetic fixture only.
	_, err = Run(context.Background(), db, in)
	require.NoError(t, err)
	after = types.User{}
	require.NoError(t, db.First(&after, "id = ?", u.ID).Error)
	require.True(t, after.IsActive)
	require.Equal(t, u.TenantID, after.TenantID)
	var n int64
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ? AND tenant_id = 8", u.ID).Count(&n).Error)
	require.EqualValues(t, 1, n)
}
