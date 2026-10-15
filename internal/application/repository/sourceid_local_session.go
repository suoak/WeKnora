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
func (r *userRepository) GetSourceIDLocalSession(ctx context.Context, id, userID string) (*types.SourceIDLocalSession, error) {
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
	return &local, nil
}

func (r *userRepository) CreateSourceIDLocalSession(ctx context.Context, session *types.SourceIDSession, local *types.SourceIDLocalSession) error {
	return r.bindingDB(ctx).Transaction(func(tx *gorm.DB) error {
		if session.SessionIndexHash != "" || session.ID != local.ID {
			return errors.New("sourceid local session invalid")
		}
		var review types.SourceIDBindingReview
		if tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer = ? AND user_id = ? AND status = ? AND subject = ?", session.Issuer, session.UserID, "approved", local.Subject).First(&review).Error != nil {
			return errors.New("sourceid local binding approval unavailable")
		}
		var identity types.ExternalIdentity
		if tx.Where("provider = ? AND issuer = ? AND user_id = ? AND employee_no = ? AND subject = ?", "sourceid", session.Issuer, session.UserID, review.EmployeeNo, local.Subject).First(&identity).Error != nil {
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
	if local.IdleSeconds <= 0 || local.AccessSeconds <= 0 || local.PolicyRef == "" || local.AdmissionPolicyRef == "" ||
		!local.AdmissionValidUntil.After(now) || !local.LastSeenAt.Add(time.Duration(local.IdleSeconds)*time.Second).After(now) {
		return errors.New("sourceid local session expired")
	}
	var count int64
	if err := tx.Session(&gorm.Session{NewDB: true}).Model(&types.ExternalIdentity{}).Where("id = ? AND provider = ? AND issuer = ? AND subject = ? AND user_id = ?", local.BindingID, "sourceid", session.Issuer, local.Subject, session.UserID).Count(&count).Error; err != nil || count != 1 {
		return errors.New("sourceid local binding revoked")
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).Model(&types.SourceIDBindingReview{}).Where("issuer = ? AND subject = ? AND user_id = ? AND status = ?", session.Issuer, local.Subject, session.UserID, "approved").Count(&count).Error; err != nil || count != 1 {
		return errors.New("sourceid local binding approval revoked")
	}
	// Conditional update prevents concurrent requests from resurrecting an idle
	// session. Never move last_seen_at backwards, including under clock skew.
	result := tx.Session(&gorm.Session{NewDB: true}).Model(&types.SourceIDLocalSession{}).
		Where("id = ? AND last_seen_at > ? AND last_seen_at <= ? AND admission_valid_until > ?", session.ID, now.Add(-time.Duration(local.IdleSeconds)*time.Second), now, now).
		Update("last_seen_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("sourceid local session expired")
	}
	return nil
}

var errSourceIDRefreshConsumed = errors.New("sourceid refresh already consumed")
