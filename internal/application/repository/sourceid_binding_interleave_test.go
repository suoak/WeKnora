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

type sourceIDReviewMissBarrier struct{}

// Pause only the losing login after its real review SELECT returned not found.
// The other login then commits the normal approved binding before enrollment
// is read. No fixture identity is bound before the losing request starts.
func TestSourceIDPilotEnrollmentAfterReviewMiss(t *testing.T) {
	runSourceIDReviewMiss(t, bindingTestDB)
}

func TestSourceIDPostgresPilotEnrollmentAfterReviewMiss(t *testing.T) {
	runSourceIDReviewMiss(t, func(t *testing.T) (*userRepository, *gorm.DB, types.User) {
		db := sourceIDPostgresDB(t)
		up, err := os.ReadFile("../../../migrations/versioned/003026_sourceid_binding_review.up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(up)).Error)
		require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "synthetic-pilot"}).Error)
		user := types.User{
			ID: uuid.NewString(), Username: "synthetic-pilot", Email: "employee@example.test",
			IsActive: true, TenantID: 42,
		}
		require.NoError(t, db.Create(&user).Error)
		return &userRepository{db: db}, db, user
	})
}

func runSourceIDReviewMiss(t *testing.T, fixture func(*testing.T) (*userRepository, *gorm.DB, types.User)) {
	t.Helper()
	for _, scenario := range []string{
		"approved", "revoked", "pending", "wrong_issuer", "wrong_employee",
		"wrong_subject", "wrong_user", "missing_review", "inactive", "enrollment_user_mismatch",
		"unreviewed_enrollment", "unapproved_pilot_owner",
		"unreviewed_review", "wrong_review_email",
	} {
		t.Run(scenario, func(t *testing.T) {
			r, db, user := fixture(t)
			accounts := map[string]types.SourceIDPilotAccount{
				user.ID: {Email: "employee@example.test", EmployeeNo: "0007"},
			}
			missed, resume := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			var intercepted atomic.Bool
			name := "test:sourceid_review_miss_" + uuid.NewString()
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Context.Value(sourceIDReviewMissBarrier{}) != true {
					return
				}
				// Storage constraints reject blank approval metadata. Inject a
				// malformed read instead, without disabling those constraints.
				if scenario == "unreviewed_enrollment" && tx.Error == nil &&
					tx.Statement.Table == "source_id_enrollments" {
					if enrollment, ok := tx.Statement.Dest.(*types.SourceIDEnrollment); ok {
						enrollment.ReviewReference = ""
					}
				}
				if tx.Statement.Table != "source_id_binding_reviews" ||
					!errors.Is(tx.Error, gorm.ErrRecordNotFound) || !intercepted.CompareAndSwap(false, true) {
					return
				}
				close(missed)
				<-resume
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
			ctx := context.WithValue(context.Background(), sourceIDReviewMissBarrier{}, true)
			requestAccounts := accounts
			if scenario == "unapproved_pilot_owner" {
				requestAccounts = map[string]types.SourceIDPilotAccount{
					"not-approved": {Email: "employee@example.test", EmployeeNo: "0007"},
				}
			}
			type result struct {
				user *types.User
				err  error
			}
			done := make(chan result, 1)
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				u, err := r.ResolveSourceIDPilotEmailIdentity(ctx, "mock-issuer", "synthetic-sub", "0007",
					"employee@example.test", "synthetic-policy", requestAccounts)
				done <- result{u, err}
			}()
			defer func() {
				release()
				select {
				case <-exited:
				case <-time.After(10 * time.Second):
					t.Error("resolver did not stop before fixture cleanup")
				}
			}()
			select {
			case <-missed:
			case <-time.After(10 * time.Second):
				t.Fatal("login did not reach review miss barrier")
			}
			if scenario == "missing_review" {
				// A concurrently enrolled legacy CSV identity is legitimate,
				// but is not an approved pilot review. Do not delete FK-linked
				// review/audit rows to manufacture an impossible database state.
				require.NoError(t, db.Create(&types.SourceIDEnrollment{
					ID: uuid.NewString(), Issuer: "mock-issuer", EmployeeNo: "0007", UserID: user.ID,
					ReviewedBy: "synthetic-csv-reviewer", ReviewReference: "synthetic-csv-ticket",
					CreatedAt: time.Now(),
				}).Error)
			} else {
				winner, err := r.ResolveSourceIDPilotEmailIdentity(context.Background(), "mock-issuer",
					"synthetic-sub", "0007", "employee@example.test", "synthetic-policy", accounts)
				require.NoError(t, err)
				require.Equal(t, user.ID, winner.ID)
			}
			otherID := uuid.NewString()
			if scenario == "wrong_user" || scenario == "enrollment_user_mismatch" {
				require.NoError(t, db.Create(&types.User{
					ID: otherID, Username: "other", Email: "other@example.test",
					IsActive: true, TenantID: user.TenantID,
				}).Error)
			}
			switch scenario {
			case "revoked", "pending":
				require.NoError(t, db.Model(&types.SourceIDBindingReview{}).
					Where("user_id = ?", user.ID).Update("status", scenario).Error)
			case "wrong_issuer", "wrong_employee", "wrong_subject":
				column := map[string]string{
					"wrong_issuer": "issuer", "wrong_employee": "employee_no", "wrong_subject": "subject",
				}[scenario]
				require.NoError(t, db.Model(&types.SourceIDBindingReview{}).
					Where("user_id = ?", user.ID).Update(column, "different").Error)
			case "wrong_user":
				require.NoError(t, db.Model(&types.SourceIDBindingReview{}).
					Where("user_id = ?", user.ID).Update("user_id", otherID).Error)
			case "inactive":
				require.NoError(t, db.Model(&user).Update("is_active", false).Error)
			case "enrollment_user_mismatch":
				require.NoError(t, db.Model(&types.SourceIDEnrollment{}).
					Where("user_id = ?", user.ID).Update("user_id", otherID).Error)
			case "unreviewed_enrollment":
				require.Error(t, db.Model(&types.SourceIDEnrollment{}).
					Where("user_id = ?", user.ID).Update("review_reference", "").Error)
			case "unreviewed_review":
				require.NoError(t, db.Model(&types.SourceIDBindingReview{}).
					Where("user_id = ?", user.ID).Update("review_reference", "").Error)
			case "wrong_review_email":
				require.NoError(t, db.Model(&types.SourceIDBindingReview{}).
					Where("user_id = ?", user.ID).Update("email", "other@example.test").Error)
			}
			var identitiesBefore, usersBefore int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&identitiesBefore).Error)
			require.NoError(t, db.Model(&types.User{}).Count(&usersBefore).Error)
			release()
			select {
			case got := <-done:
				if scenario == "approved" {
					require.NoError(t, got.err)
					require.Equal(t, user.ID, got.user.ID)
					require.Equal(t, user.IsSystemAdmin, got.user.IsSystemAdmin)
					require.Equal(t, user.CanAccessAllTenants, got.user.CanAccessAllTenants)
				} else {
					require.Error(t, got.err)
					require.Nil(t, got.user)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("login did not leave review miss barrier")
			}
			var identitiesAfter, users, sessions, tokens int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&identitiesAfter).Error)
			require.NoError(t, db.Model(&types.User{}).Count(&users).Error)
			require.NoError(t, db.Model(&types.SourceIDSession{}).Count(&sessions).Error)
			require.NoError(t, db.Model(&types.AuthToken{}).Count(&tokens).Error)
			require.Equal(t, identitiesBefore, identitiesAfter)
			require.Equal(t, usersBefore, users)
			require.Zero(t, sessions)
			require.Zero(t, tokens)
		})
	}
}
