package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizePortalCategoryAndStages(t *testing.T) {
	category, err := normalizePortalCategory("  Hardware Platforms  ")
	require.NoError(t, err)
	require.Equal(t, "hardware platforms", category)
	_, err = normalizePortalCategory(" Architecture ")
	require.ErrorIs(t, err, ErrPortalInvalidCategory)
	_, err = normalizePortalCategory("lmt")
	require.ErrorIs(t, err, ErrPortalInvalidCategory)
	stage, err := normalizePortalStage(" DESIGN ")
	require.NoError(t, err)
	require.Equal(t, "design", stage)
	_, err = normalizePortalStage("hardware")
	require.ErrorIs(t, err, ErrPortalInvalidStage)
	stage, err = normalizePortalStage("lmt")
	require.NoError(t, err)
	require.Equal(t, "lmt", stage)
}

func TestPortalStageCatalogContainsOnlyConfiguredIPDStages(t *testing.T) {
	svc := &portalService{}
	stages := svc.ListStages()
	require.Len(t, stages, 8)
	require.Equal(t, "insight", stages[0].Key)
	require.Equal(t, 10, stages[0].DisplayOrder)
	require.Equal(t, "concept_market", stages[1].Key)
	require.Equal(t, "development", stages[5].Key)
	require.Equal(t, "lmt", stages[len(stages)-1].Key)
	require.Equal(t, 80, stages[len(stages)-1].DisplayOrder)
}

func TestCreateAccessRequestAllowsTenantlessUserAndLocksPortalViewer(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-create-request")
	require.NoError(t, db.Exec(`CREATE TABLE tenant_portal_configs (
		tenant_id INTEGER PRIMARY KEY, status TEXT, display_name TEXT, description TEXT,
		category TEXT, responsible_team TEXT, contact TEXT, featured BOOLEAN, display_order INTEGER,
		allow_access_request BOOLEAN, interaction_organization_id TEXT, created_by TEXT, updated_by TEXT,
		published_at DATETIME, created_at DATETIME, updated_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE tenant_portal_stages (
		tenant_id INTEGER, stage_key TEXT, display_order INTEGER, created_at DATETIME)`).Error)
	require.NoError(t, db.Exec("INSERT INTO tenants(id, status) VALUES (11, 'active')").Error)
	require.NoError(t, db.Exec(`INSERT INTO tenant_portal_configs
		(tenant_id, status, display_name, allow_access_request, created_by, updated_by)
		VALUES (11, 'published', 'Tenantless Portal', 1, 'admin', 'admin')`).Error)

	portalRepo := repository.NewPortalRepository(db)
	audit := NewAuditLogService(repository.NewAuditLogRepository(db))
	tenantSvc := NewTenantService(repository.NewTenantRepository(db), nil)
	memberSvc := NewTenantMemberService(repository.NewTenantMemberRepository(db), audit, nil, nil)
	svc := NewPortalService(db, portalRepo, tenantSvc, memberSvc, nil, audit)

	request, err := svc.CreateAccessRequest(context.Background(), "tenantless-user", 11, "  Need platform guidance  ")
	require.NoError(t, err)
	require.Equal(t, "portal", request.Source)
	require.Equal(t, types.TenantRoleViewer, request.RequestedRole)
	require.Equal(t, "Need platform guidance", request.Reason)
	require.Equal(t, types.TenantAccessRequestPending, request.Status)

	_, err = svc.CreateAccessRequest(context.Background(), "tenantless-user", 11, "again")
	require.ErrorIs(t, err, ErrPortalPendingExists)
	var auditRow types.AuditLog
	require.NoError(t, db.Where("action = ?", types.AuditActionTenantAccessRequested).First(&auditRow).Error)
	require.NotContains(t, string(auditRow.Details), "Need platform guidance", "audit must not contain the reason body")
}

func TestApproveAccessRequestConcurrentIsIdempotent(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-approve")
	seedPortalApproval(t, db, types.TenantMemberStatusActive)
	svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.ApproveAccessRequest(context.Background(), "owner", 7, "request-1", "approved")
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var memberCount int64
	require.NoError(t, db.Table("tenant_members").Where(
		"tenant_id = ? AND user_id = ? AND role = ? AND status = ? AND deleted_at IS NULL",
		7, "applicant", types.TenantRoleViewer, types.TenantMemberStatusActive,
	).Count(&memberCount).Error)
	require.Equal(t, int64(1), memberCount)
	var request types.TenantAccessRequest
	require.NoError(t, db.First(&request, "id = ?", "request-1").Error)
	require.Equal(t, types.TenantAccessRequestApproved, request.Status)
	require.Equal(t, types.TenantRoleViewer, request.RequestedRole)

	for action, expected := range map[types.AuditAction]int64{
		types.AuditActionMemberAdded:                 1,
		types.AuditActionTenantAccessRequestApproved: 1,
	} {
		var count int64
		require.NoError(t, db.Table("audit_logs").Where("action = ?", action).Count(&count).Error)
		require.Equal(t, expected, count, action)
	}
}

func TestApproveAccessRequestRejectsSuspendedApplicant(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-suspended")
	seedPortalApproval(t, db, types.TenantMemberStatusActive)
	require.NoError(t, db.Exec(`INSERT INTO tenant_members
		(user_id, tenant_id, role, status, joined_at, created_at, updated_at)
		VALUES ('applicant', 7, 'viewer', 'suspended', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}
	err := svc.ApproveAccessRequest(context.Background(), "owner", 7, "request-1", "")
	require.ErrorIs(t, err, ErrPortalMembershipSuspended)
	var status string
	require.NoError(t, db.Table("tenant_access_requests").Select("status").Where("id = 'request-1'").Scan(&status).Error)
	require.Equal(t, "pending", status)
}

func TestApproveAccessRequestRequiresActiveOwner(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-owner")
	seedPortalApproval(t, db, types.TenantMemberStatusSuspended)
	svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}
	err := svc.ApproveAccessRequest(context.Background(), "owner", 7, "request-1", "")
	require.ErrorIs(t, err, ErrPortalReviewForbidden)
}

func TestApproveAccessRequestRequiresActiveApplicant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update string
	}{
		{name: "inactive", update: "UPDATE users SET is_active = 0 WHERE id = 'applicant'"},
		{name: "soft deleted", update: "UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'applicant'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newPortalApprovalTestDB(t, "portal-applicant-"+tc.name)
			seedPortalApproval(t, db, types.TenantMemberStatusActive)
			require.NoError(t, db.Exec(tc.update).Error)
			svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}
			err := svc.ApproveAccessRequest(context.Background(), "owner", 7, "request-1", "")
			require.ErrorIs(t, err, ErrPortalApplicantUnavailable)
			var count int64
			require.NoError(t, db.Table("tenant_members").Where("user_id = 'applicant'").Count(&count).Error)
			require.Zero(t, count)
			var status string
			require.NoError(t, db.Table("tenant_access_requests").Select("status").Where("id = 'request-1'").Scan(&status).Error)
			require.Equal(t, "pending", status)
		})
	}
}

func TestUpdatePortalConfigRejectsMissingOrSoftDeletedOrganization(t *testing.T) {
	for _, tc := range []struct {
		name  string
		orgID string
	}{
		{name: "missing", orgID: "missing-org"},
		{name: "soft deleted", orgID: "deleted-org"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newPortalApprovalTestDB(t, "portal-org-"+tc.name)
			require.NoError(t, db.Exec(`CREATE TABLE organizations (id TEXT PRIMARY KEY, deleted_at DATETIME)`).Error)
			require.NoError(t, db.Exec(`INSERT INTO organizations (id, deleted_at) VALUES ('deleted-org', CURRENT_TIMESTAMP)`).Error)
			svc := &portalService{orgRepo: repository.NewOrganizationRepository(db)}
			_, err := svc.UpdateConfig(context.Background(), "system-admin", 7, &types.PortalConfigUpdateRequest{
				DisplayName: "Portal", InteractionOrganizationID: &tc.orgID,
			})
			require.ErrorIs(t, err, ErrPortalInvalidConfig)
		})
	}
}

func TestListTenantAccessRequestsRequiresTargetActiveOwner(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-list-owner")
	seedPortalApproval(t, db, types.TenantMemberStatusActive)
	audit := NewAuditLogService(repository.NewAuditLogRepository(db))
	svc := &portalService{
		db:        db,
		repo:      repository.NewPortalRepository(db),
		tenantSvc: NewTenantService(repository.NewTenantRepository(db), nil),
		memberSvc: NewTenantMemberService(repository.NewTenantMemberRepository(db), audit, nil, nil),
	}
	rows, err := svc.ListTenantAccessRequests(context.Background(), "owner", 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, err = svc.ListTenantAccessRequests(context.Background(), "system-admin-without-membership", 7)
	require.ErrorIs(t, err, ErrPortalReviewForbidden)
}

func TestResolveInteractionUsesConfiguredOrganizationAndRealActiveTenantAccess(t *testing.T) {
	db := newPortalApprovalTestDB(t, "portal-interaction")
	for _, statement := range []string{
		`CREATE TABLE tenant_portal_configs (
			tenant_id INTEGER PRIMARY KEY, status TEXT, interaction_organization_id TEXT)`,
		`CREATE TABLE tenant_portal_stages (tenant_id INTEGER, stage_key TEXT, display_order INTEGER, created_at DATETIME)`,
		`CREATE TABLE organizations (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
		`CREATE TABLE organization_tenant_members (organization_id TEXT, tenant_id INTEGER)`,
		`INSERT INTO tenants(id, status) VALUES (7, 'active'), (8, 'active'), (11, 'active')`,
		`INSERT INTO tenant_portal_configs VALUES (11, 'published', 'org-1')`,
		`INSERT INTO organizations VALUES ('org-1', NULL)`,
		`INSERT INTO organization_tenant_members VALUES ('org-1', 7)`,
		`INSERT INTO tenant_members(user_id, tenant_id, role, status) VALUES ('user', 7, 'viewer', 'active'), ('user', 8, 'viewer', 'active')`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	audit := NewAuditLogService(repository.NewAuditLogRepository(db))
	svc := &portalService{
		db:        db,
		repo:      repository.NewPortalRepository(db),
		tenantSvc: NewTenantService(repository.NewTenantRepository(db), nil),
		memberSvc: NewTenantMemberService(repository.NewTenantMemberRepository(db), audit, nil, nil),
		orgRepo:   repository.NewOrganizationRepository(db),
	}
	path, err := svc.ResolveInteraction(context.Background(), "user", 7, 11)
	require.NoError(t, err)
	require.Equal(t, "/platform/organizations?organization_id=org-1", path)
	_, err = svc.ResolveInteraction(context.Background(), "user", 0, 11)
	require.ErrorIs(t, err, ErrPortalInteractionForbidden)
	_, err = svc.ResolveInteraction(context.Background(), "user", 8, 11)
	require.ErrorIs(t, err, ErrPortalInteractionForbidden, "active user membership alone must not replace organization tenant membership")
}

func newPortalApprovalTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), name+".db")) +
		"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, statement := range []string{
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, is_active BOOLEAN NOT NULL DEFAULT 1, deleted_at DATETIME)`,
		`CREATE TABLE tenant_members (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, tenant_id INTEGER NOT NULL,
			role TEXT NOT NULL, status TEXT NOT NULL, invited_by TEXT, joined_at DATETIME,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE UNIQUE INDEX uniq_portal_test_member ON tenant_members(user_id, tenant_id) WHERE deleted_at IS NULL`,
		`CREATE TABLE tenant_access_requests (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, applicant_user_id TEXT NOT NULL,
			source TEXT NOT NULL, status TEXT NOT NULL, reason TEXT NOT NULL, requested_role TEXT NOT NULL,
			reviewed_by TEXT, reviewed_at DATETIME, review_note TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, actor_user_id TEXT,
			actor_role TEXT, action TEXT NOT NULL, scope_type TEXT, scope_id TEXT, target_type TEXT,
			target_id TEXT, target_user_id TEXT, request_path TEXT, request_method TEXT,
			outcome TEXT, details TEXT, created_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db
}

func seedPortalApproval(t *testing.T, db *gorm.DB, ownerStatus types.TenantMemberStatus) {
	t.Helper()
	require.NoError(t, db.Exec("INSERT INTO tenants(id, status) VALUES (7, 'active')").Error)
	require.NoError(t, db.Exec("INSERT INTO users(id, is_active) VALUES ('applicant', 1)").Error)
	require.NoError(t, db.Exec(`INSERT INTO tenant_members
		(user_id, tenant_id, role, status, joined_at, created_at, updated_at)
		VALUES ('owner', 7, 'owner', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, ownerStatus).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenant_access_requests
		(id, tenant_id, applicant_user_id, source, status, reason, requested_role, created_at, updated_at)
		VALUES ('request-1', 7, 'applicant', 'portal', 'pending', 'please', 'viewer', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
}
