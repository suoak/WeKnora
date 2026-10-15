package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

func SourceIDHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func sourceIDIndexKey(issuer, index string) string { return SourceIDHash(issuer + "\x00" + index) }

// The reviewed enrollment row serializes first binding. Unique constraints are
// also mandatory, so conflicting subjects fail closed rather than rebind.
func (r *userRepository) ResolveSourceIDIdentity(ctx context.Context, issuer, subject, employee string) (*types.User, error) {
	var user types.User
	err := r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		var enrollment types.SourceIDEnrollment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("issuer = ? AND employee_no = ?", issuer, employee).First(&enrollment).Error; err != nil {
			return errors.New("sourceid enrollment requires review")
		}
		if enrollment.ReviewedBy == "" || enrollment.ReviewReference == "" {
			return errors.New("sourceid enrollment requires review")
		}
		if err := tx.Where("id = ? AND is_active = ?", enrollment.UserID, true).First(&user).Error; err != nil {
			return errors.New("sourceid account unavailable")
		}
		var identity types.ExternalIdentity
		err := tx.Where("provider = ? AND issuer = ? AND subject = ?", "sourceid", issuer, subject).First(&identity).Error
		if err == nil {
			if identity.UserID != user.ID || identity.EmployeeNo != employee {
				return errors.New("sourceid identity conflict")
			}
			return tx.Model(&identity).Updates(map[string]interface{}{"last_login_at": time.Now(), "updated_at": time.Now()}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&types.ExternalIdentity{ID: uuid.NewString(), Provider: "sourceid", Issuer: issuer, Subject: subject, EmployeeNo: employee, UserID: user.ID, CreatedAt: time.Now(), UpdatedAt: time.Now(), LastLoginAt: time.Now()}).Error
	})
	return &user, err
}

func (r *userRepository) CreateSourceIDSession(ctx context.Context, session *types.SourceIDSession) error {
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Create(session).Error
}

func checkSourceIDSession(tx *gorm.DB, id, userID string, lock bool) error {
	var session types.SourceIDSession
	query := tx.Session(&gorm.Session{NewDB: true})
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Where("id = ? AND user_id = ?", id, userID).First(&session).Error; err != nil {
		return err
	}
	if session.Revoked || !session.ExpiresAt.After(time.Now()) {
		return errors.New("sourceid session revoked")
	}
	if session.SessionIndexHash == "" {
		return checkSourceIDLocalSession(tx, &session)
	}
	var count int64
	if err := tx.Session(&gorm.Session{NewDB: true}).Model(&types.SourceIDLogout{}).Where("key = ?", session.SessionIndexHash).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("sourceid session revoked")
	}
	return nil
}

func (r *userRepository) CheckSourceIDSession(ctx context.Context, id, userID string) error {
	return checkSourceIDSession(r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}), id, userID, false)
}

func (r *userRepository) PersistSourceIDTokens(ctx context.Context, id string, access, refresh *types.AuthToken, previous string) error {
	localMode := false
	err := r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := checkSourceIDSession(tx, id, access.UserID, true); err != nil {
			return err
		}
		var session types.SourceIDSession
		if err := tx.Where("id = ? AND user_id = ?", id, access.UserID).First(&session).Error; err != nil {
			return err
		}
		localMode = session.SessionIndexHash == ""
		if localMode {
			var local types.SourceIDLocalSession
			if err := tx.Where("id = ?", id).First(&local).Error; err != nil {
				return err
			}
			if previous != "" && !local.RefreshEnabled && ctx.Value(types.SourceIDTokenOperationKey{}) != "switch" {
				return errors.New("sourceid local refresh disabled")
			}
			if access.SessionID != id || refresh.SessionID != id || refresh.UserID != access.UserID ||
				access.ExpiresAt.After(session.ExpiresAt) || access.ExpiresAt.After(local.AdmissionValidUntil) ||
				refresh.ExpiresAt.After(session.ExpiresAt) || (!local.RecheckSupported && refresh.ExpiresAt.After(local.AdmissionValidUntil)) {
				return errors.New("sourceid local token limits invalid")
			}
			if previous == "" {
				var count int64
				if err := tx.Model(&types.AuthToken{}).Where("session_id = ? AND token_type = ?", id, "refresh_token").Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return errors.New("sourceid local refresh predecessor required")
				}
			}
		}
		if previous != "" {
			query := tx.Model(&types.AuthToken{}).Where("token = ? AND user_id = ? AND session_id = ? AND is_revoked = ?", previous, access.UserID, id, false)
			if localMode {
				query = query.Where("token_type = ? AND expires_at > ?", "refresh_token", time.Now())
			}
			result := query.Update("is_revoked", true)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errSourceIDRefreshConsumed
			}
		}
		if err := tx.Create(access).Error; err != nil {
			return err
		}
		return tx.Create(refresh).Error
	})
	// Commit family revocation outside the rolled-back failed rotation.
	if localMode && errors.Is(err, errSourceIDRefreshConsumed) {
		if revokeErr := r.RevokeSourceIDSession(ctx, id, access.UserID); revokeErr != nil {
			return revokeErr
		}
	}
	return err
}

func (r *userRepository) RevokeSourceIDSession(ctx context.Context, id, userID string) error {
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.SourceIDSession{}).Where("id = ? AND user_id = ?", id, userID).Update("revoked", true).Error; err != nil {
			return err
		}
		return tx.Model(&types.AuthToken{}).Where("session_id = ? AND user_id = ?", id, userID).Update("is_revoked", true).Error
	})
}

func (r *userRepository) RevokeSourceIDIndex(ctx context.Context, issuer, index string) error {
	key := sourceIDIndexKey(issuer, index)
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.SourceIDLogout{Key: key, CreatedAt: time.Now()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.SourceIDSession{}).Where("issuer = ? AND session_index_hash = ?", issuer, key).Update("revoked", true).Error; err != nil {
			return err
		}
		return tx.Model(&types.AuthToken{}).Where("session_id IN (?)", tx.Model(&types.SourceIDSession{}).Select("id").Where("issuer = ? AND session_index_hash = ?", issuer, key)).Update("is_revoked", true).Error
	})
}

func (r *userRepository) SaveOIDCTransaction(ctx context.Context, transaction *types.OIDCTransaction) error {
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Create(transaction).Error
}

func (r *userRepository) ConsumeOIDCTransaction(ctx context.Context, key string) (*types.OIDCTransaction, error) {
	var transaction types.OIDCTransaction
	err := r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlogger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("key = ?", key).First(&transaction).Error; err != nil {
			return err
		}
		result := tx.Model(&types.OIDCTransaction{}).Where("key = ? AND consumed = ? AND expires_at > ?", key, false, time.Now()).Updates(map[string]interface{}{"consumed": true, "result": "", "verifier": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("oidc transaction expired or consumed")
		}
		return nil
	})
	return &transaction, err
}
