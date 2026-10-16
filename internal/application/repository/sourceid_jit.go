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
)

var errJIT = errors.New("sourceid jit enrollment unavailable")

// Only called after verified OIDC and explicit enterprise/JIT approval. Existing
// historical accounts NEVER fall back to JIT after a failed ownership check.
func (r *userRepository) ResolveSourceIDEnterpriseOrJITIdentity(ctx context.Context, issuer, subject, employee, email, bindingPolicy, ownershipPolicy string, maxAge time.Duration, jitPolicy string, domains []string, provision func(context.Context, interfaces.UserRepository) (*types.User, error)) (*types.User, error) {
	email, e := normalizeSourceIDEmail(email)
	if e != nil || issuer == "" || subject == "" || employee == "" || jitPolicy == "" || maxAge <= 0 || provision == nil {
		return nil, errJIT
	}
	allowed := false
	for _, domain := range domains {
		if strings.HasSuffix(email, "@"+domain) {
			allowed = true
		}
	}
	var known types.SourceIDAccountOwnership
	lookup := r.bindingDB(ctx).Where("issuer=? AND employee_no=?", issuer, employee).First(&known).Error
	if lookup == nil {
		var n int64
		if r.bindingDB(ctx).Model(&types.SourceIDJITRegistration{}).Where("issuer=? AND employee_no=?", issuer, employee).Count(&n).Error != nil {
			return nil, errJIT
		}
		if n == 0 {
			return r.ResolveSourceIDEnterpriseEmailIdentity(ctx, issuer, subject, employee, email, bindingPolicy, ownershipPolicy, maxAge)
		}
	} else if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return nil, errJIT
	}
	if !allowed {
		return nil, errJIT
	}
	var user *types.User
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		// Covers concurrent password/provider inserts too, not only cooperative
		// JIT requests. No global normalized UNIQUE index can be imposed on
		// ambiguous legacy accounts. This short registration-only table lock
		// trades JIT throughput for safety; existing logins do not take it.
		if tx.Dialector.Name() == "postgres" {
			if tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error != nil {
				return errJIT
			}
		}
		scoped := &userRepository{db: tx}
		var ownership types.SourceIDAccountOwnership
		lookup := tx.Where("issuer=? AND employee_no=?", issuer, employee).First(&ownership).Error
		if lookup == nil {
			var jit types.SourceIDJITRegistration
			jerr := tx.Where("issuer=? AND employee_no=?", issuer, employee).First(&jit).Error
			if jerr != nil && !errors.Is(jerr, gorm.ErrRecordNotFound) {
				return errJIT
			}
			if jerr == nil {
				// Only our own new accounts can renew their verified identity
				// snapshot on subsequent OIDC login, never historical ownership.
				if ownership.UserID != jit.UserID || ownership.Status != "active" || jit.PolicyRef != jitPolicy || ownership.SourceRef != "approved-jit:"+jitPolicy {
					return errJIT
				}
				var review types.SourceIDBindingReview
				if tx.Where("issuer=? AND employee_no=? AND user_id=? AND subject=? AND status='approved'", issuer, employee, jit.UserID, subject).First(&review).Error != nil {
					return errJIT
				}
				ownership.PolicyRef = ownershipPolicy
				ownership.Revision++
				ownership.VerifiedAt = time.Now().UTC()
				ownership.ValidUntil = ownership.VerifiedAt.Add(maxAge)
				if _, err := scoped.SyncSourceIDAccountOwnership(ctx, ownership, "approved-jit", true); err != nil {
					return errJIT
				}
			}
			var err error
			user, err = scoped.ResolveSourceIDEnterpriseEmailIdentity(ctx, issuer, subject, employee, email, bindingPolicy, ownershipPolicy, maxAge)
			return err
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return errJIT
		}
		var n int64
		// Include soft-deleted users: a recycled email is not a new employee.
		if tx.Unscoped().Model(&types.User{}).Where("LOWER(TRIM(email))=?", email).Count(&n).Error != nil || n != 0 {
			return errJIT
		}
		for _, q := range []struct {
			model interface{}
			where string
			args  []interface{}
		}{
			{&types.SourceIDAccountOwnership{}, "issuer=? AND email=?", []interface{}{issuer, email}},
			{&types.SourceIDJITRegistration{}, "issuer=? AND (employee_no=? OR email=?)", []interface{}{issuer, employee, email}},
			{&types.SourceIDBindingReview{}, "issuer=? AND (employee_no=? OR subject=? OR email=?)", []interface{}{issuer, employee, subject, email}},
			{&types.ExternalIdentity{}, "issuer=? AND (employee_no=? OR subject=?)", []interface{}{issuer, employee, subject}},
			{&types.SourceIDEnrollment{}, "issuer=? AND employee_no=?", []interface{}{issuer, employee}},
		} {
			if tx.Unscoped().Model(q.model).Where(q.where, q.args...).Count(&n).Error != nil || n != 0 {
				return errJIT
			}
		}
		created, err := provision(ctx, scoped)
		if err != nil || created == nil || created.ID == "" || created.Email != email || created.TenantID != 0 || !created.IsActive || created.IsSystemAdmin || created.CanAccessAllTenants {
			return errJIT
		}
		if tx.Model(&types.TenantMember{}).Where("user_id=?", created.ID).Count(&n).Error != nil || n != 0 {
			return errJIT
		}
		now := time.Now().UTC()
		if tx.Create(&types.SourceIDJITRegistration{ID: uuid.NewString(), Issuer: issuer, EmployeeNo: employee, Email: email, UserID: created.ID, PolicyRef: jitPolicy, CreatedAt: now}).Error != nil {
			return errJIT
		}
		proof := types.SourceIDAccountOwnership{Issuer: issuer, EmployeeNo: employee, Email: email, UserID: created.ID, Status: "active", Revision: 1, PolicyRef: ownershipPolicy, OwnershipRef: jitPolicy, SourceRef: "approved-jit:" + jitPolicy, VerifiedAt: now, ValidUntil: now.Add(maxAge)}
		if _, err = scoped.SyncSourceIDAccountOwnership(ctx, proof, "approved-jit", true); err != nil {
			return errJIT
		}
		user, err = scoped.ResolveSourceIDEnterpriseEmailIdentity(ctx, issuer, subject, employee, email, bindingPolicy, ownershipPolicy, maxAge)
		return err
	})
	if err != nil {
		return nil, errJIT
	}
	return user, nil
}
