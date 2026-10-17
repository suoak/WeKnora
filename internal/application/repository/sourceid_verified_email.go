package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const verifiedEmailMode = "verified_email_policy"

var errVerifiedEmail = errors.New("sourceid verified email policy unavailable")

// Serialize first linkage with password registration/email updates and JIT.
// Always users -> review -> session. Existing review/revoke takes no users
// table write lock, so revocation cannot reverse this table-lock order.
func lockSourceIDEmailAccounts(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		return tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error
	}
	return nil
}

// Existing negative/historical evidence remains authoritative in the new mode.
// Current verified email cannot override a different employee or a tombstone.
func verifiedEmailKnownEvidence(tx *gorm.DB, q types.SourceIDVerifiedEmailLogin, userID string) error {
	var n int64
	if tx.Model(&types.SourceIDLinkBlock{}).Where("issuer=? AND ((user_id<>'' AND user_id=?) OR (employee_no<>'' AND employee_no=?) OR (subject<>'' AND subject=?) OR (email<>'' AND LOWER(TRIM(email))=?))", q.Issuer, userID, q.EmployeeNo, q.Subject, q.Email).Count(&n).Error != nil || n != 0 {
		return errVerifiedEmail
	}
	var ownership []types.SourceIDAccountOwnership
	if tx.Where("issuer=? AND (employee_no=? OR user_id=? OR LOWER(TRIM(email))=?)", q.Issuer, q.EmployeeNo, userID, q.Email).Find(&ownership).Error != nil {
		return errVerifiedEmail
	}
	now := time.Now()
	for _, row := range ownership {
		if row.EmployeeNo != q.EmployeeNo || row.UserID != userID || row.Status != "active" || row.SourceRef == "" || row.OwnershipRef == "" || row.PolicyRef == "" || row.VerifiedAt.IsZero() || row.VerifiedAt.After(now) || !row.ValidUntil.After(now) {
			return errVerifiedEmail
		}
	}
	var reviews []types.SourceIDBindingReview
	if tx.Where("issuer=? AND (employee_no=? OR subject=? OR user_id=? OR LOWER(TRIM(email))=?)", q.Issuer, q.EmployeeNo, q.Subject, userID, q.Email).Find(&reviews).Error != nil {
		return errVerifiedEmail
	}
	for _, row := range reviews {
		if row.EmployeeNo != q.EmployeeNo || row.Subject != q.Subject || row.UserID != userID || (row.Status != "approved" && row.Status != "pending") || (row.Status == "pending" && row.ReviewedBy != "") {
			return errVerifiedEmail
		}
	}
	var identities []types.ExternalIdentity
	if tx.Where("provider='sourceid' AND issuer=? AND (employee_no=? OR subject=? OR user_id=?)", q.Issuer, q.EmployeeNo, q.Subject, userID).Find(&identities).Error != nil {
		return errVerifiedEmail
	}
	for _, row := range identities {
		if row.EmployeeNo != q.EmployeeNo || row.Subject != q.Subject || row.UserID != userID {
			return errVerifiedEmail
		}
	}
	var enrolled []types.SourceIDEnrollment
	if tx.Where("issuer=? AND (employee_no=? OR user_id=?)", q.Issuer, q.EmployeeNo, userID).Find(&enrolled).Error != nil {
		return errVerifiedEmail
	}
	for _, row := range enrolled {
		if row.EmployeeNo != q.EmployeeNo || row.UserID != userID {
			return errVerifiedEmail
		}
	}
	var jit []types.SourceIDJITRegistration
	if tx.Where("issuer=? AND (employee_no=? OR user_id=? OR LOWER(TRIM(email))=?)", q.Issuer, q.EmployeeNo, userID, q.Email).Find(&jit).Error != nil {
		return errVerifiedEmail
	}
	for _, row := range jit {
		if row.EmployeeNo != q.EmployeeNo || row.UserID != userID {
			return errVerifiedEmail
		}
	}
	return nil
}

// Atomic binding (or tenantless JIT), policy audit and local Session. No
// independently verified ownership is invented. All roles take this same path.
func (r *userRepository) ResolveSourceIDVerifiedEmailSession(ctx context.Context, q types.SourceIDVerifiedEmailLogin, session *types.SourceIDSession, local *types.SourceIDLocalSession, provision func(context.Context, interfaces.UserRepository) (*types.User, error)) (*types.User, bool, error) {
	email, e := normalizeSourceIDEmail(q.Email)
	if e != nil || q.Issuer == "" || q.Subject == "" || q.EmployeeNo == "" || strings.TrimSpace(q.EmployeeNo) != q.EmployeeNo || len(q.EmployeeNo) > 256 || strings.TrimSpace(q.PolicyRef) == "" || strings.TrimSpace(q.BindingPolicyRef) == "" || session == nil || local == nil || session.ID == "" || session.ID != local.ID || session.Issuer != q.Issuer || local.Subject != q.Subject || local.RefreshEnabled || session.SessionIndexHash != "" {
		return nil, false, errVerifiedEmail
	}
	q.Email = email
	var user *types.User
	created := false
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		if lockSourceIDEmailAccounts(tx) != nil {
			return errVerifiedEmail
		}
		scoped := &userRepository{db: tx}
		var review types.SourceIDBindingReview
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer=? AND (employee_no=? OR subject=?)", q.Issuer, q.EmployeeNo, q.Subject).First(&review).Error
		if lookup == nil && (review.EmployeeNo != q.EmployeeNo || review.Subject != q.Subject || (review.Status != "approved" && review.Status != "pending")) {
			return errVerifiedEmail
		}
		if lookup != nil && !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return errVerifiedEmail
		}
		decision := "existing_binding"
		if lookup == nil {
			var u types.User
			if tx.Where("id=? AND is_active=true", review.UserID).First(&u).Error != nil {
				return errVerifiedEmail
			}
			user = &u
		} else {
			// Stable external identity must never be redirected by changed email.
			var identities []types.ExternalIdentity
			if tx.Where("provider='sourceid' AND issuer=? AND (employee_no=? OR subject=?)", q.Issuer, q.EmployeeNo, q.Subject).Find(&identities).Error != nil || len(identities) > 1 {
				return errVerifiedEmail
			}
			if len(identities) == 1 {
				i := identities[0]
				if i.EmployeeNo != q.EmployeeNo || i.Subject != q.Subject {
					return errVerifiedEmail
				}
				var u types.User
				if tx.Where("id=? AND is_active=true", i.UserID).First(&u).Error != nil {
					return errVerifiedEmail
				}
				user = &u
			} else {
				var users []types.User
				if tx.Unscoped().Where("LOWER(TRIM(email))=?", email).Limit(2).Find(&users).Error != nil {
					return errVerifiedEmail
				}
				if len(users) > 1 {
					return errVerifiedEmail
				}
				if len(users) == 1 {
					if users[0].DeletedAt.Valid || !users[0].IsActive {
						return errVerifiedEmail
					}
					user = &users[0]
					decision = "first_email_match"
				} else {
					if !q.JITEnabled || q.JITPolicyRef == "" || provision == nil {
						return errVerifiedEmail
					}
					allowed := false
					for _, domain := range q.JITDomains {
						if domain != "" && strings.HasSuffix(email, "@"+domain) {
							allowed = true
						}
					}
					if !allowed || verifiedEmailKnownEvidence(tx, q, "") != nil {
						return errVerifiedEmail
					}
					u, err := provision(ctx, scoped)
					if err != nil || u == nil || u.ID == "" || u.Email != email || u.TenantID != 0 || !u.IsActive || u.IsSystemAdmin || u.CanAccessAllTenants {
						return errVerifiedEmail
					}
					var n int64
					if tx.Model(&types.TenantMember{}).Where("user_id=?", u.ID).Count(&n).Error != nil || n != 0 {
						return errVerifiedEmail
					}
					if tx.Create(&types.SourceIDJITRegistration{ID: uuid.NewString(), Issuer: q.Issuer, EmployeeNo: q.EmployeeNo, Email: email, UserID: u.ID, PolicyRef: q.JITPolicyRef, CreatedAt: time.Now()}).Error != nil {
						return errVerifiedEmail
					}
					user = u
					created = true
					decision = "jit"
				}
			}
		}
		if verifiedEmailKnownEvidence(tx, q, user.ID) != nil {
			return errVerifiedEmail
		}
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			// Seed the existing review mechanism only after all exception checks.
			// This also makes legacy enrollments pass through policy audit.
			now := time.Now()
			candidateEmail := email
			if decision == "existing_binding" {
				var e error
				candidateEmail, e = normalizeSourceIDEmail(user.Email)
				if e != nil {
					return errVerifiedEmail
				}
			}
			review = types.SourceIDBindingReview{ID: uuid.NewString(), Issuer: q.Issuer, Subject: q.Subject, EmployeeNo: q.EmployeeNo, Email: candidateEmail, UserID: user.ID, Status: "pending", CreatedAt: now, UpdatedAt: now}
			if tx.Create(&review).Error != nil {
				return errVerifiedEmail
			}
		}
		resolved, err := scoped.resolveSourceIDAutoEmailIdentity(ctx, q.Issuer, q.Subject, q.EmployeeNo, email, q.BindingPolicyRef, "company-verified-email-policy")
		if err != nil || resolved.ID != user.ID {
			return errVerifiedEmail
		}
		var approved types.SourceIDBindingReview
		if tx.Where("id=? AND status='approved'", review.ID).First(&approved).Error != nil {
			return errVerifiedEmail
		}
		d := types.SourceIDLinkDecision{ID: uuid.NewString(), ReviewID: review.ID, LinkMode: verifiedEmailMode, PolicyRef: q.PolicyRef, BindingPolicyRef: q.BindingPolicyRef, Decision: decision, CreatedAt: time.Now()}
		if tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&d).Error != nil {
			return errVerifiedEmail
		}
		// A conflicted insert retains its attempted UUID in memory. Clear it
		// before GORM lookup so it does not silently add a stale PK predicate.
		d = types.SourceIDLinkDecision{}
		if tx.Where("review_id=? AND link_mode=? AND policy_ref=? AND binding_policy_ref=?", review.ID, verifiedEmailMode, q.PolicyRef, q.BindingPolicyRef).First(&d).Error != nil {
			return errVerifiedEmail
		}
		session.UserID = user.ID
		if scoped.CreateSourceIDLocalSession(ctx, session, local) != nil {
			return errVerifiedEmail
		}
		return tx.Create(&types.SourceIDLocalLinkPolicy{ID: session.ID, DecisionID: d.ID, LinkMode: verifiedEmailMode, PolicyRef: q.PolicyRef, BindingPolicyRef: q.BindingPolicyRef}).Error
	})
	if err != nil {
		return nil, false, errVerifiedEmail
	}
	return user, created, nil
}

func (r *userRepository) SourceIDSessionLinkPolicy(ctx context.Context, id string) (*types.SourceIDLocalLinkPolicy, error) {
	db := r.bindingDB(ctx)
	// Trusted-ownership/external repositories predating 3030 remain compatible.
	if !db.Migrator().HasTable(&types.SourceIDLocalLinkPolicy{}) {
		return nil, nil
	}
	var row types.SourceIDLocalLinkPolicy
	err := db.Where("id=?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errVerifiedEmail
	}
	return &row, nil
}

func (r *userRepository) CheckSourceIDVerifiedEmailSession(ctx context.Context, id, userID, issuer, employee, subject, policy, bindingPolicy string) error {
	db := r.bindingDB(ctx)
	marker, err := r.SourceIDSessionLinkPolicy(ctx, id)
	if err != nil || marker == nil || marker.LinkMode != verifiedEmailMode || marker.PolicyRef != policy || marker.BindingPolicyRef != bindingPolicy {
		return errVerifiedEmail
	}
	var review types.SourceIDBindingReview
	if db.Where("issuer=? AND employee_no=? AND subject=? AND user_id=? AND status='approved'", issuer, employee, subject, userID).First(&review).Error != nil {
		return errVerifiedEmail
	}
	var d types.SourceIDLinkDecision
	if db.Where("id=? AND review_id=? AND link_mode=? AND policy_ref=? AND binding_policy_ref=?", marker.DecisionID, review.ID, verifiedEmailMode, policy, bindingPolicy).First(&d).Error != nil {
		return errVerifiedEmail
	}
	var user types.User
	if db.Where("id=? AND is_active=true", userID).First(&user).Error != nil {
		return errVerifiedEmail
	}
	return verifiedEmailKnownEvidence(db, types.SourceIDVerifiedEmailLogin{Issuer: issuer, EmployeeNo: employee, Subject: subject, Email: review.Email}, userID)
}

// Administrative negative evidence. No user data is returned/logged; there is
// no public API or automatic unblocking. Revoke derived sessions transactionally.
func (r *userRepository) RecordSourceIDLinkBlock(ctx context.Context, b types.SourceIDLinkBlock) error {
	if b.Issuer == "" || strings.TrimSpace(b.Actor) == "" || strings.TrimSpace(b.Reference) == "" || (b.UserID == "" && b.EmployeeNo == "" && b.Subject == "" && b.Email == "") {
		return errVerifiedEmail
	}
	if b.Reason != "email_reassigned" && b.Reason != "ownership_conflict" && b.Reason != "identity_disabled" && b.Reason != "security_hold" {
		return errVerifiedEmail
	}
	if b.Email != "" {
		var err error
		b.Email, err = normalizeSourceIDEmail(b.Email)
		if err != nil {
			return errVerifiedEmail
		}
	}
	b.ID = uuid.NewString()
	b.CreatedAt = time.Now()
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		if lockSourceIDEmailAccounts(tx) != nil {
			return errVerifiedEmail
		}
		if tx.Create(&b).Error != nil {
			return errVerifiedEmail
		}
		users := tx.Model(&types.SourceIDBindingReview{}).Select("user_id").Where("issuer=? AND ((user_id<>'' AND user_id=?) OR (employee_no<>'' AND employee_no=?) OR (subject<>'' AND subject=?) OR (email<>'' AND LOWER(TRIM(email))=?))", b.Issuer, b.UserID, b.EmployeeNo, b.Subject, b.Email)
		if tx.Model(&types.SourceIDSession{}).Where("issuer=? AND user_id IN (?)", b.Issuer, users).Update("revoked", true).Error != nil {
			return errVerifiedEmail
		}
		ids := tx.Model(&types.SourceIDSession{}).Select("id").Where("issuer=? AND user_id IN (?)", b.Issuer, users)
		return tx.Model(&types.AuthToken{}).Where("session_id IN (?)", ids).Update("is_revoked", true).Error
	})
	if err != nil {
		return errVerifiedEmail
	}
	return nil
}
