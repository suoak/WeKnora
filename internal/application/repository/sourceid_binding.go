package repository

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

var ErrSourceIDBindingPending = errors.New("sourceid binding approval required")

func normalizeSourceIDEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 254 {
		return "", errors.New("sourceid email invalid")
	}
	a, err := mail.ParseAddress(value)
	if err != nil || a.Address != value || a.Name != "" {
		return "", errors.New("sourceid email invalid")
	}
	// Keep SQL lower/trim normalization portable. Unicode/alias rules need a
	// separate, explicitly approved enterprise contract, not guesswork.
	for _, c := range value {
		if c > 127 || c <= 32 {
			return "", errors.New("sourceid email invalid")
		}
	}
	return value, nil
}

func (r *userRepository) bindingDB(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard})
}

type sourceIDPilotScopeKey struct{}
type sourceIDPilotScope map[string]types.SourceIDPilotAccount

// Applies the same protected pilot/account-ownership boundary to every role.
// It must run before a pending review, identity or enrollment is inserted.
func checkSourceIDPilotAccount(ctx context.Context, userID, email, employee string, first bool) error {
	accounts, scoped := ctx.Value(sourceIDPilotScopeKey{}).(sourceIDPilotScope)
	if !scoped {
		return nil
	} // Existing modes keep their approved behavior.
	approved, ok := accounts[userID]
	if !ok || approved.Email == "" || approved.EmployeeNo == "" || employee != approved.EmployeeNo {
		return errors.New("sourceid pilot account unavailable")
	}
	if first {
		normalized, err := normalizeSourceIDEmail(email)
		if err != nil || normalized != approved.Email {
			return errors.New("sourceid account ownership unconfirmed")
		}
	}
	return nil
}

func (r *userRepository) ResolveSourceIDPilotEmailIdentity(ctx context.Context, issuer, subject, employee, email, policy string, accounts map[string]types.SourceIDPilotAccount) (*types.User, error) {
	if len(accounts) == 0 {
		return nil, errors.New("sourceid pilot account unavailable")
	}
	copyAccounts := sourceIDPilotScope{}
	for id, email := range accounts {
		copyAccounts[id] = email
	}
	return r.ResolveSourceIDAutoEmailIdentity(context.WithValue(ctx, sourceIDPilotScopeKey{}, copyAccounts), issuer, subject, employee, email, policy)
}

// Approved identity wins over email. A newly discovered candidate is committed
// as pending, with no external identity, enrollment, session or JWT created.
func (r *userRepository) ResolveSourceIDEmailIdentity(ctx context.Context, issuer, subject, employee, email string) (*types.User, error) {
	if issuer == "" || subject == "" || employee == "" {
		return nil, errors.New("sourceid identity missing")
	}
	db := r.bindingDB(ctx)
	var review types.SourceIDBindingReview
	err := db.Where("issuer = ? AND (employee_no = ? OR subject = ?)", issuer, employee, subject).First(&review).Error
	if err == nil {
		if review.EmployeeNo != employee || review.Subject != subject {
			return nil, errors.New("sourceid identity conflict")
		}
		if review.Status == "revoked" {
			return nil, errors.New("sourceid binding revoked")
		}
		if review.Status != "approved" {
			if err := checkSourceIDPilotAccount(ctx, review.UserID, review.Email, employee, true); err != nil {
				return nil, err
			}
			return nil, ErrSourceIDBindingPending
		}
		if err := checkSourceIDPilotAccount(ctx, review.UserID, "", employee, false); err != nil {
			return nil, err
		}
		return r.ResolveSourceIDIdentity(ctx, issuer, subject, employee)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("sourceid review lookup failed")
	}
	// Preserve independently reviewed legacy CSV mappings. Existing subject
	// conflicts are still rejected by ResolveSourceIDIdentity.
	var enrollment types.SourceIDEnrollment
	err = db.Where("issuer = ? AND employee_no = ?", issuer, employee).First(&enrollment).Error
	if err == nil {
		if _, scoped := ctx.Value(sourceIDPilotScopeKey{}).(sourceIDPilotScope); scoped {
			return nil, errors.New("sourceid pilot reviewed identity required")
		}
		if err := checkSourceIDPilotAccount(ctx, enrollment.UserID, "", employee, false); err != nil {
			return nil, err
		}
		return r.ResolveSourceIDIdentity(ctx, issuer, subject, employee)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("sourceid enrollment lookup failed")
	}
	var existing int64
	if db.Model(&types.ExternalIdentity{}).Where("provider = ? AND issuer = ? AND (employee_no = ? OR subject = ?)", "sourceid", issuer, employee, subject).Count(&existing).Error != nil {
		return nil, errors.New("sourceid identity lookup failed")
	}
	if existing != 0 {
		var concurrent types.SourceIDBindingReview
		if db.Where("issuer = ? AND employee_no = ? AND subject = ? AND status = ?", issuer, employee, subject, "approved").First(&concurrent).Error == nil {
			if err := checkSourceIDPilotAccount(ctx, concurrent.UserID, "", employee, false); err != nil {
				return nil, err
			}
			return r.ResolveSourceIDIdentity(ctx, issuer, subject, employee)
		}
		return nil, errors.New("sourceid identity conflict")
	}
	normalized, err := normalizeSourceIDEmail(email)
	if err != nil {
		return nil, err
	}
	var candidates []types.User
	if db.Where("LOWER(TRIM(email)) = ?", normalized).Limit(2).Find(&candidates).Error != nil {
		return nil, errors.New("sourceid candidate lookup failed")
	}
	if len(candidates) != 1 || !candidates[0].IsActive {
		return nil, errors.New("sourceid email match unavailable")
	}
	user := candidates[0]
	if err := checkSourceIDPilotAccount(ctx, user.ID, normalized, employee, true); err != nil {
		return nil, err
	}
	var conflicts int64
	if db.Model(&types.ExternalIdentity{}).Where("provider = ? AND issuer = ? AND user_id = ?", "sourceid", issuer, user.ID).Count(&conflicts).Error != nil {
		return nil, errors.New("sourceid identity lookup failed")
	}
	if conflicts != 0 {
		var concurrent types.SourceIDBindingReview
		if db.Where("issuer = ? AND employee_no = ? AND subject = ? AND user_id = ? AND status = ?", issuer, employee, subject, user.ID, "approved").First(&concurrent).Error == nil {
			if err := checkSourceIDPilotAccount(ctx, concurrent.UserID, "", employee, false); err != nil {
				return nil, err
			}
			return r.ResolveSourceIDIdentity(ctx, issuer, subject, employee)
		}
		return nil, errors.New("sourceid identity conflict")
	}
	now := time.Now()
	review = types.SourceIDBindingReview{ID: uuid.NewString(), Issuer: issuer, Subject: subject, EmployeeNo: employee, UserID: user.ID, Email: normalized, Status: "pending", CreatedAt: now, UpdatedAt: now}
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&review)
	if result.Error != nil {
		return nil, errors.New("sourceid candidate conflict; review required")
	}
	if result.RowsAffected == 0 {
		var existing types.SourceIDBindingReview
		if db.Where("issuer = ? AND employee_no = ? AND subject = ? AND user_id = ?", issuer, employee, subject, user.ID).First(&existing).Error != nil || existing.Status == "revoked" {
			return nil, errors.New("sourceid identity conflict")
		}
	}
	return nil, ErrSourceIDBindingPending
}

func (r *userRepository) ResolveSourceIDAutoEmailIdentity(ctx context.Context, issuer, subject, employee, email, policyReference string) (*types.User, error) {
	return r.resolveSourceIDAutoEmailIdentity(ctx, issuer, subject, employee, email, policyReference, "enterprise-auto-policy")
}

func (r *userRepository) resolveSourceIDAutoEmailIdentity(ctx context.Context, issuer, subject, employee, email, policyReference, actor string) (*types.User, error) {
	if strings.TrimSpace(policyReference) == "" {
		return nil, ErrSourceIDBindingPending
	}
	user, err := r.ResolveSourceIDEmailIdentity(ctx, issuer, subject, employee, email)
	if err == nil || !errors.Is(err, ErrSourceIDBindingPending) {
		return user, err
	}
	var row types.SourceIDBindingReview
	if r.bindingDB(ctx).Where("issuer = ? AND employee_no = ? AND subject = ?", issuer, employee, subject).First(&row).Error != nil {
		return nil, ErrSourceIDBindingPending
	}
	if err := r.reviewSourceIDBinding(ctx, row.ID, actor, policyReference, "approve", true); err != nil {
		return nil, ErrSourceIDBindingPending
	}
	return r.ResolveSourceIDEmailIdentity(ctx, issuer, subject, employee, email)
}

func (r *userRepository) ListSourceIDBindingReviews(ctx context.Context) ([]types.SourceIDBindingReview, error) {
	var reviews []types.SourceIDBindingReview
	err := r.bindingDB(ctx).Order("created_at DESC").Limit(100).Find(&reviews).Error
	return reviews, err
}

// Use the same review-row lock as revocation so a login resolved just before
// revocation cannot create a fresh, still-valid session after revocation.
func (r *userRepository) CreateSourceIDReviewedSession(ctx context.Context, session *types.SourceIDSession) error {
	return r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		var row types.SourceIDBindingReview
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer = ? AND user_id = ?", session.Issuer, session.UserID).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Explicit legacy CSV approval is independently reviewed too.
			var enrollment types.SourceIDEnrollment
			if tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer = ? AND user_id = ?", session.Issuer, session.UserID).First(&enrollment).Error != nil || enrollment.ReviewedBy == "" || enrollment.ReviewReference == "" {
				return errors.New("sourceid session approval unavailable")
			}
		} else if err != nil || row.Status != "approved" {
			return errors.New("sourceid session approval unavailable")
		}
		return tx.Create(session).Error
	})
}

// Operator-only API: callers must enforce admin authorization outside this
// repository. Reviewer/reference document independent account-ownership review.
func (r *userRepository) ReviewSourceIDBinding(ctx context.Context, id, reviewer, reference, action string) error {
	return r.reviewSourceIDBinding(ctx, id, reviewer, reference, action, false)
}

func (r *userRepository) reviewSourceIDBinding(ctx context.Context, id, reviewer, reference, action string, automatic bool) error {
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reference) == "" {
		return errors.New("sourceid approval evidence required")
	}
	if action != "approve" && action != "revoke" && action != "reopen" {
		return errors.New("sourceid review action invalid")
	}
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		var row types.SourceIDBindingReview
		if tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error != nil {
			return errors.New("sourceid review missing")
		}
		if automatic && row.Status == "approved" {
			return nil
		}
		if automatic && row.ReviewedBy != "" {
			return ErrSourceIDBindingPending
		}
		// Rolled back with any rejected action, including duplicate approval.
		if err := tx.Create(&types.SourceIDBindingAudit{ID: uuid.NewString(), ReviewID: row.ID, Action: action, ReviewedBy: reviewer, ReviewReference: reference, CreatedAt: time.Now()}).Error; err != nil {
			return err
		}
		if action == "reopen" {
			if row.Status != "revoked" {
				return errors.New("sourceid review state conflict")
			}
			var user types.User
			if tx.Where("id = ? AND is_active = ?", row.UserID, true).First(&user).Error != nil {
				return errors.New("sourceid account unavailable")
			}
			email, err := normalizeSourceIDEmail(user.Email)
			if err != nil {
				return err
			}
			var count int64
			if tx.Model(&types.User{}).Where("LOWER(TRIM(email)) = ?", email).Count(&count).Error != nil || count != 1 {
				return errors.New("sourceid email ambiguous")
			}
			// Explicit administrator re-review keeps the same user_id; an email
			// change alone can never transfer the identity to a different user.
			return tx.Model(&row).Updates(map[string]interface{}{"status": "pending", "email": email, "reviewed_by": reviewer, "review_reference": reference, "updated_at": time.Now()}).Error
		}
		if action == "revoke" {
			if err := tx.Model(&types.SourceIDSession{}).Where("issuer = ? AND user_id = ?", row.Issuer, row.UserID).Update("revoked", true).Error; err != nil {
				return err
			}
			if err := tx.Model(&types.AuthToken{}).Where("user_id = ? AND session_id IN (?)", row.UserID, tx.Model(&types.SourceIDSession{}).Select("id").Where("issuer = ? AND user_id = ?", row.Issuer, row.UserID)).Update("is_revoked", true).Error; err != nil {
				return err
			}
			if err := tx.Where("provider = ? AND issuer = ? AND user_id = ? AND employee_no = ?", "sourceid", row.Issuer, row.UserID, row.EmployeeNo).Delete(&types.ExternalIdentity{}).Error; err != nil {
				return err
			}
			if err := tx.Where("issuer = ? AND user_id = ? AND employee_no = ?", row.Issuer, row.UserID, row.EmployeeNo).Delete(&types.SourceIDEnrollment{}).Error; err != nil {
				return err
			}
			return tx.Model(&row).Updates(map[string]interface{}{"status": "revoked", "reviewed_by": reviewer, "review_reference": reference, "updated_at": time.Now()}).Error
		}
		if row.Status != "pending" {
			return errors.New("sourceid review state conflict")
		}
		var user types.User
		if tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND is_active = ?", row.UserID, true).First(&user).Error != nil {
			return errors.New("sourceid account unavailable")
		}
		if err := checkSourceIDPilotAccount(ctx, user.ID, user.Email, row.EmployeeNo, true); err != nil {
			return err
		}
		// The approved enterprise policy covers every existing account role.
		// Identity approval never edits the user's privileges or memberships.
		// The locked active-account, unique-email and identity-conflict checks
		// below apply equally to automatic and operator-reviewed approval.
		normalized, err := normalizeSourceIDEmail(user.Email)
		if err != nil || normalized != row.Email {
			return errors.New("sourceid candidate email changed; new review required")
		}
		var count int64
		if tx.Model(&types.User{}).Where("LOWER(TRIM(email)) = ?", normalized).Count(&count).Error != nil || count != 1 {
			return errors.New("sourceid candidate email ambiguous")
		}
		var enrollments []types.SourceIDEnrollment
		if tx.Where("issuer = ? AND (employee_no = ? OR user_id = ?)", row.Issuer, row.EmployeeNo, row.UserID).Find(&enrollments).Error != nil {
			return errors.New("sourceid enrollment lookup failed")
		}
		for _, e := range enrollments {
			if e.EmployeeNo != row.EmployeeNo || e.UserID != row.UserID {
				return errors.New("sourceid identity conflict")
			}
		}
		if len(enrollments) == 0 {
			if err := tx.Create(&types.SourceIDEnrollment{ID: uuid.NewString(), Issuer: row.Issuer, EmployeeNo: row.EmployeeNo, UserID: row.UserID, ReviewedBy: reviewer, ReviewReference: reference, CreatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		var identities []types.ExternalIdentity
		if tx.Where("provider = ? AND issuer = ? AND (employee_no = ? OR subject = ? OR user_id = ?)", "sourceid", row.Issuer, row.EmployeeNo, row.Subject, row.UserID).Find(&identities).Error != nil {
			return errors.New("sourceid identity lookup failed")
		}
		for _, i := range identities {
			if i.EmployeeNo != row.EmployeeNo || i.Subject != row.Subject || i.UserID != row.UserID {
				return errors.New("sourceid identity conflict")
			}
		}
		if len(identities) == 0 {
			now := time.Now()
			if err := tx.Create(&types.ExternalIdentity{ID: uuid.NewString(), Provider: "sourceid", Issuer: row.Issuer, Subject: row.Subject, EmployeeNo: row.EmployeeNo, UserID: row.UserID, CreatedAt: now, UpdatedAt: now, LastLoginAt: now}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&row).Where("status = ?", "pending").Updates(map[string]interface{}{"status": "approved", "reviewed_by": reviewer, "review_reference": reference, "updated_at": time.Now()}).Error
	})
	if err != nil {
		return errors.New("sourceid review rejected; transaction rolled back")
	}
	return nil
}
