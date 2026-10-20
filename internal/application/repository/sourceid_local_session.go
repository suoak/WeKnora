package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// An empty index is an explicitly local session, never an external-session
// fallback. Missing migration/metadata therefore fails closed.
func (r *userRepository) GetSourceIDLocalSession(
	ctx context.Context,
	id, userID string,
) (*types.SourceIDLocalSession, error) {
	var session types.SourceIDSession
	db := r.bindingDB(ctx)
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&session).Error; err != nil {
		return nil, err
	}
	if session.SessionIndexHash != "" {
		return nil, nil
	}
	var local types.SourceIDLocalSession
	if err := db.Where("id = ?", id).First(&local).Error; err != nil {
		return nil, errors.New("sourceid local session metadata unavailable")
	}
	local.Issuer = session.Issuer
	local.AbsoluteExpiresAt = session.ExpiresAt
	local.Revoked = session.Revoked
	var identity types.ExternalIdentity
	if err := db.Where(
		"id = ? AND subject = ? AND user_id = ? AND issuer = ?",
		local.BindingID,
		local.Subject,
		userID,
		session.Issuer,
	).
		First(
			&identity,
		).
		Error; err != nil {
		return nil, errors.New("sourceid local identity unavailable")
	}
	local.EmployeeNo = identity.EmployeeNo
	return &local, nil
}

func (r *userRepository) CreateSourceIDLocalSession(
	ctx context.Context,
	session *types.SourceIDSession,
	local *types.SourceIDLocalSession,
) error {
	return r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		if session.SessionIndexHash != "" || session.ID != local.ID {
			return errors.New("sourceid local session invalid")
		}
		var review types.SourceIDBindingReview
		if tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"issuer = ? AND user_id = ? AND status = ? AND subject = ?",
				session.Issuer, session.UserID, "approved", local.Subject,
			).
			First(&review).
			Error != nil {
			return errors.New("sourceid local binding approval unavailable")
		}
		var identity types.ExternalIdentity
		if tx.Where("provider = ? AND issuer = ? AND user_id = ? AND employee_no = ? AND "+
			"subject = ?", "sourceid", session.Issuer, session.UserID, review.EmployeeNo, local.Subject).
			First(&identity).
			Error != nil {
			return errors.New("sourceid local binding unavailable")
		}
		local.BindingID = identity.ID
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		return tx.Create(local).Error
	})
}

func checkSourceIDLocalSession(tx *gorm.DB, session *types.SourceIDSession) error {
	var local types.SourceIDLocalSession
	if tx.Session(&gorm.Session{NewDB: true}).Where("id = ?", session.ID).First(&local).Error != nil {
		return errors.New("sourceid local session metadata unavailable")
	}
	now := time.Now()
	if local.IdleSeconds <= 0 || local.AccessSeconds <= 0 || local.PolicyRef == "" ||
		local.AdmissionPolicyRef == "" ||
		local.LastSeenAt.After(now) ||
		!local.AdmissionValidUntil.After(now) ||
		!local.LastSeenAt.Add(time.Duration(local.IdleSeconds)*time.Second).After(now) {
		return errors.New("sourceid local session expired")
	}
	var count int64
	if err := tx.Session(
		&gorm.Session{
			NewDB: true,
		},
	).
		Model(
			&types.ExternalIdentity{},
		).
		Where(
			"id = ? AND provider = ? AND issuer = ? AND subject = ? AND user_id = ?",
			local.BindingID,
			"sourceid",
			session.Issuer,
			local.Subject,
			session.UserID,
		).
		Count(
			&count,
		).
		Error; err != nil ||
		count != 1 {
		return errors.New("sourceid local binding revoked")
	}
	if err := tx.Session(
		&gorm.Session{
			NewDB: true,
		},
	).
		Model(
			&types.SourceIDBindingReview{},
		).
		Where(
			"issuer = ? AND subject = ? AND user_id = ? AND status = ?",
			session.Issuer,
			local.Subject,
			session.UserID,
			"approved",
		).
		Count(
			&count,
		).
		Error; err != nil ||
		count != 1 {
		return errors.New("sourceid local binding approval revoked")
	}
	// Conditional update prevents concurrent requests from resurrecting an idle
	// session. Never move last_seen_at backwards, including under clock skew.
	result := tx.Session(&gorm.Session{NewDB: true}).Model(&types.SourceIDLocalSession{}).
		Where("id = ? AND last_seen_at > ? AND admission_valid_until > ?", session.ID, now.Add(
			-time.Duration(
				local.IdleSeconds,
			)*time.Second,
		), now).
		Update("last_seen_at", gorm.Expr("CASE WHEN last_seen_at < ? THEN ? ELSE last_seen_at END", now, now))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("sourceid local session expired")
	}
	return nil
}

var errSourceIDRefreshConsumed = errors.New("sourceid refresh already consumed")

func (r *userRepository) UpdateSourceIDLocalAdmission(
	ctx context.Context,
	id, userID string,
	decision types.SourceIDAdmissionDecision,
) error {
	return r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		var base types.SourceIDSession
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).
			Where(
				"id = ? AND user_id = ?",

				id,

				userID,
			).
			First(
				&base,
			).
			Error; err != nil {
			return err
		}
		var local types.SourceIDLocalSession
		if err := tx.Where("id = ?", id).First(&local).Error; err != nil {
			return err
		}
		now := time.Now()
		if base.Revoked || !base.ExpiresAt.After(now) || !local.RecheckSupported ||
			!local.LastSeenAt.Add(time.Duration(local.IdleSeconds)*time.Second).After(now) ||
			decision.Outcome != types.SourceIDAdmissionAllow ||
			!decision.RecheckSupported ||
			decision.Source != local.AdmissionSource ||

			decision.ContractRef != local.AdmissionPolicyRef || decision.CheckedAt.Before(local.AdmissionCheckedAt) ||
			!decision.ValidUntil.After(now) {
			return errors.New("sourceid admission renewal rejected")
		}
		until := decision.ValidUntil
		if until.After(base.ExpiresAt) {
			until = base.ExpiresAt
		}
		return tx.Model(&local).
			Updates(map[string]interface{}{"admission_checked_at": decision.CheckedAt, "admission_valid_until": until}).
			Error
	})
}
