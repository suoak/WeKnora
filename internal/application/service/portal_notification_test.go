package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func portalNotificationRequestFixture(t *testing.T) (*gorm.DB, interfaces.PortalService) {
	t.Helper()
	db := newPortalApprovalTestDB(t, "request-notifications")
	require.NoError(t, db.Exec("DROP TABLE tenant_portal_configs").Error)
	require.NoError(t, db.AutoMigrate(&types.TenantPortalConfig{}, &types.TenantPortalStage{}))
	require.NoError(t, db.Exec("ALTER TABLE tenants ADD COLUMN name TEXT").Error)
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,status) VALUES(7,'Portal','active')").Error)
	require.NoError(
		t,
		db.Exec(
			"INSERT INTO users(id,username) VALUES('owner','Owner'),('owner2','Owner Two'),('applicant','Applicant')",
		).Error,
	)
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO tenant_members(user_id,tenant_id,role,status)
			VALUES('owner',7,'owner','active'),('owner2',7,'owner','active')`,
		).Error,
	)
	require.NoError(
		t,
		db.Exec(
			`CREATE UNIQUE INDEX uq_pending_request
			ON tenant_access_requests(tenant_id,applicant_user_id) WHERE status='pending'`,
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.TenantPortalConfig{
				TenantID:           7,
				DisplayName:        "Portal",
				Status:             types.PortalStatusPublished,
				AllowAccessRequest: true,
				CreatedBy:          "owner",
				UpdatedBy:          "owner",
			},
		).Error,
	)
	audit := NewAuditLogService(repository.NewAuditLogRepository(db))
	return db, NewPortalService(
		db,
		repository.NewPortalRepository(db),
		NewTenantService(repository.NewTenantRepository(db), nil),
		NewTenantMemberService(repository.NewTenantMemberRepository(db), audit, nil, nil),
		nil,
		audit,
	)
}

func TestPortalResolutionClosesAllOwnerRemindersAndKeepsHistory(t *testing.T) {
	for _, outcome := range []string{"approved", "rejected", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			db, svc := portalNotificationRequestFixture(t)
			ctx := context.Background()
			request, err := svc.CreateAccessRequest(ctx, "applicant", 7, "multi-owner regression")
			require.NoError(t, err)
			repo := repository.NewNotificationRepository(db)
			rows, _, err := repo.List(ctx, "owner2", 1, 20, false)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, types.TenantAccessRequestPending, rows[0].RequestStatus)

			// Reading one inbox must not affect the other recipient. Resolution
			// must preserve that first read time and unrelated unread messages.
			readAt := time.Now().UTC().Add(-time.Hour)
			require.NoError(t, db.Model(&types.UserNotification{}).Where("user_id = ?", "owner").Updates(map[string]any{"is_read": true, "read_at": readAt}).Error)
			require.NoError(t, db.Create(&types.UserNotification{ID: "unrelated", UserID: "owner2", NotificationType: "other", ReferenceID: "other", TenantID: 8}).Error)
			switch outcome {
			case "approved":
				require.NoError(t, svc.ApproveAccessRequest(ctx, "owner", 7, request.ID, "approved"))
				// A second Owner's retry is idempotent; it cannot replace the
				// original reviewer or send another applicant notification.
				require.NoError(t, svc.ApproveAccessRequest(ctx, "owner2", 7, request.ID, "retry"))
				require.ErrorIs(t, svc.RejectAccessRequest(ctx, "owner2", 7, request.ID, "stale"), ErrPortalRequestNotPending)
			case "rejected":
				require.NoError(t, svc.RejectAccessRequest(ctx, "owner", 7, request.ID, "rejected"))
				require.ErrorIs(t, svc.ApproveAccessRequest(ctx, "owner2", 7, request.ID, "stale"), ErrPortalRequestNotPending)
			case "cancelled":
				require.NoError(t, svc.CancelAccessRequest(ctx, "applicant", request.ID))
				require.ErrorIs(t, svc.ApproveAccessRequest(ctx, "owner2", 7, request.ID, "stale"), ErrPortalRequestNotPending)
			}
			for _, owner := range []string{"owner", "owner2"} {
				rows, _, err := repo.List(ctx, owner, 1, 20, false)
				require.NoError(t, err)
				var found bool
				for _, row := range rows {
					if row.ReferenceID != request.ID {
						continue
					}
					found = true
					require.True(t, row.IsRead)
					require.NotNil(t, row.ReadAt)
					require.Equal(t, types.TenantAccessRequestStatus(outcome), row.RequestStatus)
					if owner == "owner" {
						require.WithinDuration(t, readAt, *row.ReadAt, time.Millisecond)
					}
					if outcome != "cancelled" {
						require.Equal(t, "owner", *row.ReviewedBy)
						require.Equal(t, "Owner", row.ReviewerName)
						require.NotNil(t, row.ReviewedAt)
					}
				}
				require.True(t, found, "resolved reminders remain in history")
			}
			n, err := repo.UnreadCount(ctx, "owner2")
			require.NoError(t, err)
			require.EqualValues(t, 1, n, "unrelated unread notification must remain")
			unread, _, err := repo.List(ctx, "owner2", 1, 20, true)
			require.NoError(t, err)
			require.Len(t, unread, 1)
			require.Equal(t, "unrelated", unread[0].ID)
			if outcome != "cancelled" {
				n, err := repo.UnreadCount(ctx, "applicant")
				require.NoError(t, err)
				require.EqualValues(t, 1, n, "the applicant result remains unread")
			}
		})
	}
}

func TestPortalOwnerReminderResolutionRollsBackWithResultDelivery(t *testing.T) {
	db, svc := portalNotificationRequestFixture(t)
	ctx := context.Background()
	request, err := svc.CreateAccessRequest(ctx, "applicant", 7, "atomic resolution")
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_result_notification BEFORE INSERT ON user_notifications
		WHEN NEW.user_id='applicant' BEGIN SELECT RAISE(ABORT,'synthetic delivery failure'); END`).Error)
	require.Error(t, svc.ApproveAccessRequest(ctx, "owner", 7, request.ID, "approve"))
	var saved types.TenantAccessRequest
	require.NoError(t, db.First(&saved, "id = ?", request.ID).Error)
	require.Equal(t, types.TenantAccessRequestPending, saved.Status)
	repo := repository.NewNotificationRepository(db)
	for _, owner := range []string{"owner", "owner2"} {
		n, err := repo.UnreadCount(ctx, owner)
		require.NoError(t, err)
		require.EqualValues(t, 1, n, "rolled-back approval must leave both reminders actionable")
	}
	var count int64
	require.NoError(t, db.Table("tenant_members").Where("user_id = ?", "applicant").Count(&count).Error)
	require.Zero(t, count)
	// Cancellation uses the same atomic reminder resolution contract.
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_reminder_resolution BEFORE UPDATE ON user_notifications
		BEGIN SELECT RAISE(ABORT,'synthetic reminder failure'); END`).Error)
	require.Error(t, svc.CancelAccessRequest(ctx, "applicant", request.ID))
	require.NoError(t, db.First(&saved, "id = ?", request.ID).Error)
	require.Equal(t, types.TenantAccessRequestPending, saved.Status)
}

func TestNotificationRequestMetadataRespectsTenantBinding(t *testing.T) {
	db, svc := portalNotificationRequestFixture(t)
	ctx := context.Background()
	request, err := svc.CreateAccessRequest(ctx, "applicant", 7, "tenant-bound state")
	require.NoError(t, err)
	require.NoError(t, svc.ApproveAccessRequest(ctx, "owner", 7, request.ID, "approve"))
	require.NoError(t, db.Create(&types.UserNotification{ID: "foreign-reference", UserID: "owner2", TenantID: 8,
		NotificationType: "other", ReferenceType: "tenant_access_request", ReferenceID: request.ID}).Error)
	rows, _, err := repository.NewNotificationRepository(db).List(ctx, "owner2", 1, 20, false)
	require.NoError(t, err)
	for _, row := range rows {
		if row.ID == "foreign-reference" {
			require.Empty(t, row.RequestStatus)
			require.Nil(t, row.ReviewedBy)
			require.Empty(t, row.ReviewerName)
		}
	}
}

func TestPortalRequestNotificationCreationAndRollback(t *testing.T) {
	db, svc := portalNotificationRequestFixture(t)
	ctx := context.Background()
	req, err := svc.CreateAccessRequest(ctx, "applicant", 7, "Need access")
	require.NoError(t, err)
	var rows []types.UserNotification
	require.NoError(t, db.Order("user_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "owner", rows[0].UserID)
	require.Equal(t, "owner2", rows[1].UserID)
	require.Equal(t, req.ID, rows[0].ReferenceID)
	require.Contains(t, rows[0].Content, "Applicant")
	_, err = svc.CreateAccessRequest(ctx, "applicant", 7, "duplicate")
	require.ErrorIs(t, err, ErrPortalPendingExists)
	var n int64
	require.NoError(t, db.Table("tenant_members").Where("user_id='applicant'").Count(&n).Error)
	require.Zero(t, n, "submission must never authorize membership")
	require.NoError(t, svc.CancelAccessRequest(ctx, "applicant", req.ID))
	require.NoError(t, db.Exec("DROP TABLE user_notifications").Error)
	_, err = svc.CreateAccessRequest(ctx, "applicant", 7, "notification write fails")
	require.Error(t, err)
	require.NoError(t, db.Table("tenant_access_requests").Where("status='pending'").Count(&n).Error)
	require.Zero(t, n)
	require.NoError(
		t,
		db.Table("audit_logs").Where("action = ?", types.AuditActionTenantAccessRequested).Count(&n).Error,
	)
	require.EqualValues(t, 1, n, "failed creation must not add an audit record")
}

func TestPortalRequestStateAndTextBoundaries(t *testing.T) {
	db, svc := portalNotificationRequestFixture(t)
	ctx := context.Background()
	for _, reason := range []string{"", " \n\t", strings.Repeat("字", 1001), "hello\x00world", "hidden\u202econtrol"} {
		_, err := svc.CreateAccessRequest(ctx, "applicant", 7, reason)
		require.ErrorIs(t, err, ErrPortalInvalidReason)
	}
	require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET allow_access_request=false WHERE tenant_id=7").Error)
	_, err := svc.CreateAccessRequest(ctx, "applicant", 7, "valid")
	require.ErrorIs(t, err, ErrPortalNotRequestable)
	require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET allow_access_request=true WHERE tenant_id=7").Error)
	require.NoError(
		t,
		db.Exec(
			"INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('applicant',7,'viewer','suspended')",
		).Error,
	)
	_, err = svc.CreateAccessRequest(ctx, "applicant", 7, "valid")
	require.ErrorIs(t, err, ErrPortalMembershipSuspended)
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='applicant'").Error)
	_, err = svc.CreateAccessRequest(ctx, "applicant", 7, "valid")
	require.ErrorIs(t, err, ErrPortalAlreadyMember)
	for _, note := range []string{strings.Repeat("字", 1001), "bad\x00note", "bad\u202enote"} {
		require.ErrorIs(t, svc.RejectAccessRequest(ctx, "owner", 7, "missing", note), ErrPortalInvalidConfig)
	}
}

func TestPortalConfigDefaultsAndExplicitOptOut(t *testing.T) {
	db, svc := portalNotificationRequestFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("DELETE FROM tenant_portal_configs").Error)
	config, err := svc.GetAdminSpace(ctx, 7)
	require.NoError(t, err)
	require.True(t, config.AllowAccessRequest)
	config, err = svc.UpdateConfig(ctx, "owner", 7, &types.PortalConfigUpdateRequest{DisplayName: "Portal"})
	require.NoError(t, err)
	require.True(t, config.AllowAccessRequest)
	closed := false
	config, err = svc.UpdateConfig(
		ctx,
		"owner",
		7,
		&types.PortalConfigUpdateRequest{DisplayName: "Portal", AllowAccessRequest: &closed},
	)
	require.NoError(t, err)
	require.False(t, config.AllowAccessRequest)
	config, err = svc.UpdateConfig(ctx, "owner", 7, &types.PortalConfigUpdateRequest{DisplayName: "Portal renamed"})
	require.NoError(t, err)
	require.False(t, config.AllowAccessRequest, "omission must preserve existing opt-out")
}

func TestPortalReviewNotificationsAreAtomicAndIdempotent(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approve", false: "reject"}[approve], func(t *testing.T) {
			db := newPortalApprovalTestDB(t, "notification-review")
			seedPortalApproval(t, db, types.TenantMemberStatusActive)
			svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}
			for range 2 {
				require.NoError(
					t,
					svc.reviewAccessRequest(context.Background(), "owner", 7, "request-1", "review note", approve),
				)
			}
			var rows []types.UserNotification
			require.NoError(t, db.Find(&rows).Error)
			require.Len(t, rows, 1)
			require.Equal(t, "applicant", rows[0].UserID)
			require.Contains(t, rows[0].Content, "review note")
			var members int64
			require.NoError(t, db.Table("tenant_members").Where("user_id = ?", "applicant").Count(&members).Error)
			if approve {
				require.EqualValues(t, 1, members)
			} else {
				require.Zero(t, members)
			}
		})
	}

	db := newPortalApprovalTestDB(t, "notification-failure")
	seedPortalApproval(t, db, types.TenantMemberStatusActive)
	require.NoError(t, db.Exec("DROP TABLE user_notifications").Error)
	svc := &portalService{db: db, repo: repository.NewPortalRepository(db)}
	require.Error(t, svc.ApproveAccessRequest(context.Background(), "owner", 7, "request-1", "note"))
	var request types.TenantAccessRequest
	require.NoError(t, db.First(&request, "id = ?", "request-1").Error)
	require.Equal(t, types.TenantAccessRequestPending, request.Status)
	var n int64
	require.NoError(t, db.Table("tenant_members").Where("user_id = ?", "applicant").Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Table("audit_logs").Count(&n).Error)
	require.Zero(t, n)
}

func TestPortalSubmittedNotificationOnlyForAllEffectiveOwners(t *testing.T) {
	db := newPortalApprovalTestDB(t, "notification-owners")
	seedPortalApproval(t, db, types.TenantMemberStatusActive)
	for _, id := range []string{"owner", "owner2", "suspended", "inactive", "admin"} {
		require.NoError(
			t,
			db.Exec("INSERT INTO users(id,username,is_active) VALUES(?,?,?)", id, id, id != "inactive").Error,
		)
	}
	for _, row := range []struct{ id, role, status string }{
		{"owner2", "owner", "active"},
		{"suspended", "owner", "suspended"},
		{"inactive", "owner", "active"},
		{"admin", "admin", "active"},
	} {
		require.NoError(
			t,
			db.Exec(
				"INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES(?,7,?,?)",
				row.id,
				row.role,
				row.status,
			).Error,
		)
	}
	var request types.TenantAccessRequest
	require.NoError(t, db.First(&request, "id = ?", "request-1").Error)
	for range 2 {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return repository.NewNotificationRepository(tx).
				AccessRequestEvent(context.Background(), &request, "access_request_submitted", "")
		}))
	}
	var rows []types.UserNotification
	require.NoError(t, db.Order("user_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "owner", rows[0].UserID)
	require.Equal(t, "owner2", rows[1].UserID)
	require.Contains(t, rows[0].Content, request.Reason)
}
