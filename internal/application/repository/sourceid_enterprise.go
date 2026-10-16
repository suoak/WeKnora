package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errOwnership = errors.New("sourceid enterprise ownership unavailable")
var errOwnershipDryRun = errors.New("sourceid ownership dry run")

func ownershipUsable(row *types.SourceIDAccountOwnership, userID, policy string, maxAge time.Duration) bool {
	now := time.Now()
	return maxAge > 0 && row.UserID == userID && row.Status == "active" && row.PolicyRef == policy &&
		row.OwnershipRef != "" && row.SourceRef != "" && !row.VerifiedAt.After(now) && row.VerifiedAt.Add(maxAge).After(now) && row.ValidUntil.After(now)
}

func enterpriseOwnership(tx *gorm.DB, issuer, employee, userID, policy string, maxAge time.Duration, lock bool) (*types.SourceIDAccountOwnership, error) {
	var row types.SourceIDAccountOwnership
	if lock {
		tx = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if tx.Where("issuer = ? AND employee_no = ?", issuer, employee).First(&row).Error != nil {
		return nil, errOwnership
	}
	if userID == "" {
		userID = row.UserID
	}
	if !ownershipUsable(&row, userID, policy, maxAge) {
		return nil, errOwnership
	}
	return &row, nil
}

func (r *userRepository) CheckSourceIDEnterpriseOwnership(ctx context.Context, issuer, employee, userID, policy string, maxAge time.Duration) error {
	_, err := enterpriseOwnership(r.bindingDB(ctx), issuer, employee, userID, policy, maxAge, false)
	return err
}

// Indexed lookup of one protected proof, then reuse the existing all-role
// approval transaction. No population-sized manifest is loaded on login.
func (r *userRepository) ResolveSourceIDEnterpriseEmailIdentity(ctx context.Context, issuer, subject, employee, email, bindingPolicy, ownershipPolicy string, maxAge time.Duration) (*types.User, error) {
	var user *types.User
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := enterpriseOwnership(tx, issuer, employee, "", ownershipPolicy, maxAge, true)
		if err != nil {
			return err
		}
		scoped := &userRepository{db: tx}
		user, err = scoped.ResolveSourceIDPilotEmailIdentity(ctx, issuer, subject, employee, email, bindingPolicy, map[string]types.SourceIDPilotAccount{row.UserID: {Email: row.Email, EmployeeNo: row.EmployeeNo}})
		return err
	})
	if err != nil {
		return nil, errOwnership
	}
	return user, nil
}

// Serializes session creation against ownership disable/sync. Existing local
// session/token/revocation implementations remain the source of truth.
func (r *userRepository) CreateSourceIDEnterpriseLocalSession(ctx context.Context, session *types.SourceIDSession, local *types.SourceIDLocalSession, employee, policy string, maxAge time.Duration) error {
	err := r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := enterpriseOwnership(tx, session.Issuer, employee, session.UserID, policy, maxAge, true); err != nil {
			return err
		}
		return (&userRepository{db: tx}).CreateSourceIDLocalSession(ctx, session, local)
	})
	if err != nil {
		return errOwnership
	}
	return nil
}

// Operator-only, per-row atomic incremental sync. References attest trusted
// independent evidence at the administrative boundary; they are not proof
// inferred from a token. Never log/serialize input records or SQL errors.
func (r *userRepository) SyncSourceIDAccountOwnership(ctx context.Context, input types.SourceIDAccountOwnership, actor string, apply bool) (changed bool, err error) {
	email, e := normalizeSourceIDEmail(input.Email)
	if e != nil || strings.TrimSpace(actor) == "" || input.Issuer == "" || input.EmployeeNo == "" || strings.TrimSpace(input.EmployeeNo) != input.EmployeeNo || len(input.EmployeeNo) > 256 || input.UserID == "" ||
		input.Revision <= 0 || strings.TrimSpace(input.PolicyRef) == "" || strings.TrimSpace(input.OwnershipRef) == "" || strings.TrimSpace(input.SourceRef) == "" || (input.Status != "active" && input.Status != "disabled") ||
		input.VerifiedAt.IsZero() || input.VerifiedAt.After(time.Now()) || (input.Status == "active" && (!input.ValidUntil.After(time.Now()) || !input.ValidUntil.After(input.VerifiedAt))) {
		return false, errOwnership
	}
	input.Email = email
	// PostgreSQL timestamps have microsecond resolution. Canonicalize before
	// persistence/comparison so an identical source revision remains idempotent
	// across supported databases, including nanosecond JSON timestamps.
	input.VerifiedAt = input.VerifiedAt.UTC().Truncate(time.Microsecond)
	input.ValidUntil = input.ValidUntil.UTC().Truncate(time.Microsecond)
	err = r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		var user types.User
		if tx.Where("id = ?", input.UserID).First(&user).Error != nil {
			return errOwnership
		}
		if input.Status == "active" && !user.IsActive {
			return errOwnership
		}
		var count int64
		if tx.Model(&types.User{}).Where("LOWER(TRIM(email)) = ?", email).Count(&count).Error != nil {
			return errOwnership
		}
		var approved types.SourceIDBindingReview
		bound := tx.Where("issuer = ? AND employee_no = ? AND user_id = ? AND status = ?", input.Issuer, input.EmployeeNo, input.UserID, "approved").First(&approved).Error == nil
		if !bound {
			original, e := normalizeSourceIDEmail(user.Email)
			if input.Status == "active" && (e != nil || original != email || count != 1) {
				return errOwnership
			}
		}
		now := time.Now()
		input.ID = uuid.NewString()
		input.CreatedAt = now
		input.UpdatedAt = now
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&input)
		if result.Error != nil {
			return errOwnership
		}
		if result.RowsAffected == 0 {
			var row types.SourceIDAccountOwnership
			if tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer = ? AND employee_no = ?", input.Issuer, input.EmployeeNo).First(&row).Error != nil || row.UserID != input.UserID {
				return errOwnership
			}
			input.ID = row.ID
			if input.Revision < row.Revision {
				return errOwnership
			}
			if input.Revision == row.Revision {
				if row.Email != email || row.Status != input.Status || row.PolicyRef != input.PolicyRef || row.SourceRef != input.SourceRef || row.OwnershipRef != input.OwnershipRef || !row.VerifiedAt.Equal(input.VerifiedAt) || !row.ValidUntil.Equal(input.ValidUntil) {
					return errOwnership
				}
				return nil
			}
			if tx.Model(&row).Updates(map[string]interface{}{"email": email, "status": input.Status, "revision": input.Revision, "policy_ref": input.PolicyRef, "source_ref": input.SourceRef, "ownership_ref": input.OwnershipRef, "verified_at": input.VerifiedAt, "valid_until": input.ValidUntil, "updated_at": now}).Error != nil {
				return errOwnership
			}
		}
		if input.Status == "disabled" {
			if tx.Model(&types.SourceIDSession{}).Where("issuer = ? AND user_id = ?", input.Issuer, input.UserID).Update("revoked", true).Error != nil {
				return errOwnership
			}
			ids := tx.Model(&types.SourceIDSession{}).Select("id").Where("issuer = ? AND user_id = ?", input.Issuer, input.UserID)
			if tx.Model(&types.AuthToken{}).Where("session_id IN (?)", ids).Update("is_revoked", true).Error != nil {
				return errOwnership
			}
		}
		if tx.Create(&types.SourceIDOwnershipAudit{ID: uuid.NewString(), OwnershipID: input.ID, Revision: input.Revision, Actor: actor, Action: input.Status, CreatedAt: now}).Error != nil {
			return errOwnership
		}
		changed = true
		if !apply {
			return errOwnershipDryRun
		}
		return nil
	})
	if errors.Is(err, errOwnershipDryRun) {
		return changed, nil
	}
	if err != nil {
		return false, errOwnership
	}
	return changed, nil
}
