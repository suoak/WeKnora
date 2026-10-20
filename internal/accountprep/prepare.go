// Package accountprep is an operator-only, single-account fixture command's
// transaction boundary. It is deliberately not wired into the HTTP server.
package accountprep

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlog "gorm.io/gorm/logger"

	repo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	preparedAction types.AuditAction = "sourceid.test_account_prepared"
	cleanedAction  types.AuditAction = "sourceid.test_account_detached"
)

// Input contains PII only in memory. Never marshal or log it.
type Input struct {
	Email, OperationID, OperatorID, Approval string
	TenantID                                 uint64
	Role                                     types.TenantRole
	Apply, ContributorApproved               bool
	Cleanup                                  bool
}

// Result is the entire console allowlist. Identity and credentials are absent.
type Result struct {
	DryRun         bool `json:"dry_run"`
	CreateUser     bool `json:"create_user"`
	AddMembership  bool `json:"add_membership"`
	AlreadyApplied bool `json:"already_applied"`
	Cleanup        bool `json:"cleanup"`
}

type baseline struct {
	Created, Added  bool
	HomeChanged     bool
	Home            uint64
	MemberID        uint64
	Role            types.TenantRole
	Approval        string
	CleanupApproval string `json:",omitempty"`
}

var reference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,95}$`)

// Run never migrates, creates a tenant, grants privilege, or deletes a user.
// All repositories and existing business services share this one transaction.
func Run(ctx context.Context, db *gorm.DB, in Input) (out Result, err error) {
	out.DryRun, out.Cleanup = !in.Apply, in.Cleanup
	if in.Role == "" {
		in.Role = types.TenantRoleViewer
	}
	if in.Role != types.TenantRoleViewer &&
		(in.Role != types.TenantRoleContributor || !in.ContributorApproved) {
		return out, errors.New("role_not_approved")
	}
	if _, e := uuid.Parse(in.OperationID); e != nil {
		return out, errors.New("operation_id_required")
	}
	if _, e := uuid.Parse(in.OperatorID); e != nil || in.TenantID == 0 ||
		!reference.MatchString(in.Approval) {
		return out, errors.New("operator_tenant_approval_required")
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	a, e := mail.ParseAddress(in.Email)
	if e != nil || a.Address != in.Email || len(in.Email) > 254 {
		return out, errors.New("email_invalid")
	}
	// Service errors can include driver/PII text. Suppress both business and SQL
	// logging for this invocation, not globally for any running server.
	l := logrus.New()
	l.SetOutput(io.Discard)
	ctx = context.WithValue(ctx, types.LoggerContextKey, logrus.NewEntry(l))
	db = db.Session(&gorm.Session{Logger: gormlog.Discard}).WithContext(ctx)
	if db.Name() != "postgres" && db.Name() != "sqlite" {
		return out, errors.New("unsupported_database")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		// A single global operator lock serializes even two operations with
		// differently cased email or different operation IDs. No PII lock hash.
		if tx.Name() == "postgres" {
			if tx.Exec("SELECT pg_advisory_xact_lock(84302701)").Error != nil {
				return errors.New("lock_unavailable")
			}
		}
		var tenant types.Tenant
		if tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&tenant, "id = ? AND status = ?", in.TenantID, "active").
			Error != nil {
			return errors.New("target_space_unavailable")
		}
		var owner types.TenantMember
		if tx.First(
			&owner,
			"tenant_id = ? AND user_id = ? AND role = ? AND status = ?",
			in.TenantID,
			in.OperatorID,
			"owner",
			"active",
		).Error != nil {
			return errors.New("space_owner_required")
		}
		var actor types.User
		if tx.First(&actor, "id = ? AND is_active = ?", in.OperatorID, true).Error != nil {
			return errors.New("space_owner_required")
		}
		for _, table := range []string{
			"users",
			"tenant_members",
			"organization_tenant_members",
			"external_identities",
			"source_id_binding_reviews",
			"source_id_enrollments",
			"source_id_sessions",
			"auth_tokens",
			"audit_logs",
		} {
			if !tx.Migrator().HasTable(table) {
				return errors.New("required_schema_missing")
			}
		}
		var users []types.User
		if tx.Unscoped().
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("LOWER(TRIM(email)) = ?", in.Email).
			Find(&users).
			Error != nil {
			return errors.New("lookup_failed")
		}
		if len(users) > 1 {
			return errors.New("email_ambiguous")
		}
		var prior []types.AuditLog
		if tx.Where("scope_id = ? AND action IN ?", in.OperationID, []types.AuditAction{preparedAction, cleanedAction}).
			Order("id").
			Find(&prior).
			Error != nil {
			return errors.New("audit_unavailable")
		}
		if len(prior) > 1 {
			return errors.New("operation_already_closed")
		}
		var b baseline
		if len(prior) == 1 {
			if prior[0].TenantID != in.TenantID || prior[0].ActorUserID != in.OperatorID ||
				len(users) != 1 ||
				users[0].ID != prior[0].TargetUserID ||
				json.Unmarshal(prior[0].Details, &b) != nil ||
				b.Role != in.Role {
				return errors.New("operation_conflict")
			}
		}
		ur := repo.NewUserRepository(tx)
		tr := repo.NewAuthTokenRepository(tx)
		members := service.NewTenantMemberService(repo.NewTenantMemberRepository(tx), nil, ur, tr)
		if in.Cleanup {
			if len(prior) != 1 {
				return errors.New("operation_not_found")
			}
			if checkOrdinary(tx, users[0]) != nil || identityConflict(tx, users[0].ID) != nil {
				return errors.New("cleanup_requires_review")
			}
			if b.Added {
				var member types.TenantMember
				if tx.First(
					&member,
					"id = ? AND tenant_id = ? AND user_id = ? AND role = ? AND status = ?",
					b.MemberID,
					in.TenantID,
					users[0].ID,
					b.Role,
					"active",
				).Error != nil {
					return errors.New("baseline_changed")
				}
			}
			expectedHome := b.Home
			if b.HomeChanged {
				expectedHome = in.TenantID
			}
			if b.Created {
				var n int64
				if tx.Unscoped().
					Model(&types.TenantMember{}).
					Where("user_id = ?", users[0].ID).
					Count(&n).
					Error != nil ||
					n != 1 {
					return errors.New("baseline_changed")
				}
			}
			if users[0].TenantID != expectedHome {
				return errors.New("baseline_changed")
			}
			if !in.Apply {
				return nil
			}
			if !b.Added && !b.HomeChanged {
				return record(ctx, tx, in, users[0].ID, b, cleanedAction)
			}
			// Revoke strictly before best-effort member-service cleanup.
			if tr.RevokeTokensByUserID(ctx, users[0].ID) != nil {
				return errors.New("revoke_failed")
			}
			if b.Added && members.RemoveMember(ctx, users[0].ID, in.TenantID) != nil {
				return errors.New("detach_failed")
			}
			// Never delete an employee account. New fixtures are deactivated;
			// existing accounts remain active and retain their original home.
			u, e := ur.GetUserByID(ctx, users[0].ID)
			if e != nil {
				return errors.New("lookup_failed")
			}
			u.TenantID = b.Home
			if b.Created {
				u.IsActive = false
			}
			if ur.UpdateUser(ctx, u) != nil {
				return errors.New("restore_failed")
			}
			return record(ctx, tx, in, u.ID, b, cleanedAction)
		}
		if len(users) == 1 {
			if checkOrdinary(tx, users[0]) != nil || identityConflict(tx, users[0].ID) != nil {
				return errors.New("account_requires_review")
			}
		}
		if len(prior) == 1 {
			var member types.TenantMember
			if b.Approval != in.Approval ||
				tx.First(
					&member,
					"id = ? AND tenant_id = ? AND user_id = ? AND role = ? AND status = ?",
					b.MemberID,
					in.TenantID,
					users[0].ID,
					in.Role,
					"active",
				).Error != nil {
				return errors.New("baseline_changed")
			}
			out.AlreadyApplied = true
			return nil
		}
		out.CreateUser, out.AddMembership = len(users) == 0, true
		if len(users) == 1 {
			var member types.TenantMember
			res := tx.Unscoped().
				Where("tenant_id = ? AND user_id = ?", in.TenantID, users[0].ID).
				First(&member)
			if res.Error == nil {
				if member.DeletedAt.Valid || member.Role != in.Role || member.Status != "active" {
					return errors.New("membership_conflict")
				}
				out.AddMembership = false
			} else if !errors.Is(res.Error, gorm.ErrRecordNotFound) {
				return errors.New("lookup_failed")
			}
		}
		if !in.Apply {
			return nil
		}
		var u *types.User
		b = baseline{
			Created:  out.CreateUser,
			Added:    out.AddMembership,
			Role:     in.Role,
			Approval: in.Approval,
		}
		if out.CreateUser {
			password := make([]byte, 24)
			if _, e := rand.Read(password); e != nil {
				return errors.New("random_unavailable")
			}
			svc := service.NewUserService(&config.Config{}, ur, tr, nil, members, nil)
			u, e = svc.Register(
				ctx,
				&types.RegisterRequest{
					Email:              in.Email,
					Username:           "oa-test-" + uuid.NewString(),
					Password:           base64.RawURLEncoding.EncodeToString(password),
					TenantProvisioning: types.TenantProvisioningTenantless,
				},
			)
			if e != nil {
				return errors.New("register_failed")
			}
		} else {
			u = &users[0]
			b.Home = u.TenantID
		}
		if out.AddMembership {
			member, e := members.AddMember(ctx, u.ID, in.TenantID, in.Role, &in.OperatorID)
			if e != nil {
				return errors.New("membership_failed")
			}
			b.MemberID = member.ID
		} else {
			var member types.TenantMember
			if tx.First(&member, "user_id = ? AND tenant_id = ?", u.ID, in.TenantID).Error != nil {
				return errors.New("lookup_failed")
			}
			b.MemberID = member.ID
		}
		if out.CreateUser || u.TenantID == 0 {
			b.HomeChanged = true
			u.TenantID = in.TenantID
			if ur.UpdateUser(ctx, u) != nil {
				return errors.New("home_failed")
			}
		}
		return record(ctx, tx, in, u.ID, b, preparedAction)
	})
	if err != nil {
		// Never export GORM/driver errors from commit/rollback or service calls.
		return Result{DryRun: !in.Apply, Cleanup: in.Cleanup}, errors.New("preparation_rejected")
	}
	return out, nil
}

func record(
	ctx context.Context,
	tx *gorm.DB,
	in Input,
	userID string,
	b baseline,
	action types.AuditAction,
) error {
	if action == cleanedAction {
		b.CleanupApproval = in.Approval
	}
	data, _ := json.Marshal(b)
	return repo.NewAuditLogRepository(tx).
		Create(
			ctx,

			&types.AuditLog{
				TenantID: in.TenantID,

				ActorUserID: in.OperatorID,

				ActorRole: "owner",

				Action: action,

				ScopeType: "test_account",

				ScopeID: in.OperationID,

				TargetUserID: userID,

				Outcome: types.AuditOutcomeSuccess,

				Details: types.JSON(
					data,
				),

				CreatedAt: time.Now(),
			},
		)
}

func checkOrdinary(tx *gorm.DB, u types.User) error {
	if u.DeletedAt.Valid || !u.IsActive || u.IsSystemAdmin || u.CanAccessAllTenants {
		return errors.New("privileged_or_disabled")
	}
	var n int64
	if tx.Table("tenant_members").
		Where("user_id = ? AND (role NOT IN ? OR status <> ? OR deleted_at IS NOT "+
			"NULL)", u.ID, []string{"viewer", "contributor"}, "active").
		Count(&n).
		Error != nil ||
		n != 0 {
		return errors.New("membership_requires_review")
	}
	if u.TenantID != 0 {
		if tx.Model(&types.TenantMember{}).
			Where("user_id = ? AND tenant_id = ? AND status = ?", u.ID, u.TenantID, "active").
			Count(&n).
			Error != nil ||
			n != 1 {
			return errors.New("home_membership_missing")
		}
	}
	if tx.Table("organization_tenant_members").
		Where("role = ? AND (representative_user_id = ? OR tenant_id = ? OR "+
			"tenant_id IN (SELECT tenant_id FROM tenant_members WHERE user_id = ?))", "admin", u.ID, u.TenantID, u.ID).
		Count(&n).
		Error != nil ||
		n != 0 {
		return errors.New("organization_privilege")
	}
	return nil
}

func identityConflict(tx *gorm.DB, userID string) error {
	for _, table := range []string{
		"external_identities",

		"source_id_binding_reviews",

		"source_id_enrollments",

		"source_id_sessions",
	} {

		var n int64
		if tx.Table(table).Where("user_id = ?", userID).Count(&n).Error != nil || n != 0 {
			return errors.New("identity_requires_review")
		}

	}
	return nil
}
