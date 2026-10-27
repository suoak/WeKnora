package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const feishuHealthPrefix = "integration.feishu.health."

type feishuHealthProbeKey struct{}

// Stored encrypted in the existing settings table; never contains raw tokens.
// The epoch fences every probe/event result against subsequent consent.
type feishuConnectionHealth struct {
	AppID, OpenID, Epoch, TenantKey              string
	State, ErrorCategory                         string
	CheckedAt, CheckAfter, ConsentAt, LeaseUntil time.Time
	Lease                                        string
}

func feishuHealthEnabled() bool { return os.Getenv("FEISHU_PERSONAL_CONNECTION_CHECKS_ENABLED") == "1" }
func feishuHealthKey(user string) string {
	h := sha256.Sum256([]byte(user))
	return feishuHealthPrefix + hex.EncodeToString(h[:])
}
func loadFeishuHealth(tx *gorm.DB, user string) (*types.SystemSetting, *feishuConnectionHealth, error) {
	var row types.SystemSetting
	if err := tx.Where("key = ?", feishuHealthKey(user)).First(&row).Error; err != nil {
		return nil, nil, err
	}
	cipher, err := row.AsString()
	if err != nil {
		return nil, nil, err
	}
	var health feishuConnectionHealth
	if err = openFeishu(cipher, &health); err != nil {
		return nil, nil, err
	}
	return &row, &health, nil
}
func storeFeishuHealth(tx *gorm.DB, user string, h *feishuConnectionHealth) error {
	cipher, err := sealFeishu(h)
	if err != nil {
		return err
	}
	value, err := json.Marshal(cipher)
	if err != nil {
		return err
	}
	row := types.SystemSetting{Key: feishuHealthKey(user), Value: value, ValueType: "string", Category: "integration", IsSecret: true, Description: "Personal Feishu connection health and epoch fence", LastModifiedBy: user}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "is_secret", "updated_at"})}).Create(&row).Error
}

// Must be called after user/connection authentication, including at commit.
func (s *FeishuPersonalService) assertConnectionHealth(tx *gorm.DB, user string) error {
	if !feishuHealthEnabled() {
		return nil
	}
	_, h, err := loadFeishuHealth(tx, user)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var epoch types.FeishuPersonalGrantEpoch
	if err = tx.Where("user_id = ?", user).First(&epoch).Error; err != nil {
		return ErrFeishuReconnect
	}
	if epoch.Epoch != h.Epoch {
		return nil
	}
	if h.State == "reauthorize" {
		return ErrFeishuReconnect
	}
	if h.State == "unknown" {
		return core.ErrTransient
	}
	return nil
}

// Network calls never occur under a DB lock. The lease coordinates APP/workers;
// the result transaction locks the current connection and checks the epoch/nonce.
func (s *FeishuPersonalService) inspectConnection(ctx context.Context, user string) (*feishuConnectionHealth, error) {
	cfg, err := s.app(ctx)
	if err != nil || !cfg.modeEnabled(types.FeishuUserOAuth) {
		return nil, ErrFeishuReconnect
	}
	var conn types.FeishuConnection
	var epoch types.FeishuPersonalGrantEpoch
	if err = s.db.WithContext(ctx).Where("user_id = ? AND app_id = ?", user, cfg.AppID).First(&conn).Error; err != nil {
		return nil, ErrFeishuReconnect
	}
	if err = s.db.WithContext(ctx).Where("user_id = ?", user).First(&epoch).Error; err != nil {
		return nil, ErrFeishuReconnect
	}
	nonce := uuid.NewString()
	claimed := false
	var snapshot *feishuConnectionHealth
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Same lock order as OAuth completion and Disconnect.
		var current types.FeishuConnection
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND app_id = ? AND open_id = ?", user, conn.AppID, conn.OpenID).First(&current).Error; e != nil {
			return ErrFeishuReconnect
		}
		var grant types.FeishuPersonalGrantEpoch
		if e := tx.Where("user_id = ? AND epoch = ?", user, epoch.Epoch).First(&grant).Error; e != nil {
			return ErrFeishuReconnect
		}
		_, h, e := loadFeishuHealth(tx, user)
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if h == nil || h.Epoch != epoch.Epoch || h.AppID != conn.AppID || h.OpenID != conn.OpenID {
			h = &feishuConnectionHealth{AppID: conn.AppID, OpenID: conn.OpenID, Epoch: epoch.Epoch, State: "unknown", ConsentAt: time.Now()}
		}
		snapshot = h
		if h.CheckAfter.After(time.Now()) || h.LeaseUntil.After(time.Now()) {
			return nil
		}
		h.Lease, h.LeaseUntil = nonce, time.Now().Add(20*time.Second)
		if e = storeFeishuHealth(tx, user, h); e != nil {
			return e
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return snapshot, err
	}
	probe, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	probe = context.WithValue(probe, feishuHealthProbeKey{}, true)
	probe = context.WithValue(probe, types.UserIDContextKey, user)
	remote := core.WithUserIdentity(probe, func(call context.Context) (string, error) { return s.UserToken(call, user, conn.AppID, conn.OpenID) })
	var info struct {
		Data struct {
			OpenID    string `json:"open_id"`
			TenantKey string `json:"tenant_key"`
		} `json:"data"`
	}
	err = s.client(&core.Config{AppID: conn.AppID}).DoRequest(remote, "GET", "/open-apis/authen/v1/user_info", nil, &info)
	state, category := "connected", ""
	if err != nil {
		state, category = "unknown", feishuRemoteFailureCategory(err)
		// A document 403/scope error is never a global revocation verdict. Here we
		// queried the dedicated identity endpoint; definite refresh rejection is final.
		if errors.Is(err, ErrFeishuRefreshExpired) || errors.Is(err, ErrFeishuRefreshRejected) {
			state = "reauthorize"
		}
	} else if info.Data.OpenID != conn.OpenID {
		state, category = "reauthorize", "authorization_invalid"
	}
	persistErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.FeishuConnection
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND app_id = ? AND open_id = ?", user, conn.AppID, conn.OpenID).First(&current).Error; e != nil {
			return ErrFeishuReconnect
		}
		var grant types.FeishuPersonalGrantEpoch
		if e := tx.Where("user_id = ? AND epoch = ?", user, epoch.Epoch).First(&grant).Error; e != nil {
			return ErrFeishuReconnect
		}
		_, h, e := loadFeishuHealth(tx, user)
		if e != nil {
			return e
		}
		if h.Epoch != epoch.Epoch || h.Lease != nonce {
			return ErrFeishuReconnect
		}
		h.State, h.ErrorCategory, h.CheckedAt = state, category, time.Now()
		if state == "connected" {
			h.TenantKey = info.Data.TenantKey
		}
		h.CheckAfter = time.Now().Add(5 * time.Minute)
		if state == "unknown" {
			h.CheckAfter = time.Now().Add(time.Minute)
		}
		h.Lease = ""
		h.LeaseUntil = time.Time{}
		snapshot = h
		return storeFeishuHealth(tx, user, h)
	})
	return snapshot, persistErr
}

// Refresh bounded pages; DB leases prevent duplicate requests across workers.
// The dispatch reconciler invokes this regularly even when browser pages close.
func (s *FeishuPersonalService) reconcileConnectionHealth(ctx context.Context) error {
	if !feishuHealthEnabled() {
		return nil
	}
	cfg, err := s.app(ctx)
	if err != nil || !cfg.modeEnabled(types.FeishuUserOAuth) {
		return nil
	}
	var users []string
	if err = s.db.WithContext(ctx).Model(&types.FeishuConnection{}).Where("app_id = ?", cfg.AppID).Order("user_id").Limit(100).Pluck("user_id", &users).Error; err != nil {
		return err
	}
	for _, user := range users {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A remote failure is a connection state, not a reason to stop unrelated work.
		if _, err = s.inspectConnection(ctx, user); err != nil && !errors.Is(err, ErrFeishuReconnect) {
			return err
		}
	}
	return nil
}

// A failed refresh is fenced by the exact encrypted credential snapshot. A
// concurrent successful refresh/reconnection cannot be marked invalid by it.
func (s *FeishuPersonalService) noteConnectionFailure(ctx context.Context, old *types.FeishuConnection, cause error) {
	if !feishuHealthEnabled() || old == nil || (!errors.Is(cause, ErrFeishuRefreshExpired) && !errors.Is(cause, ErrFeishuRefreshRejected)) {
		return
	}
	_ = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.FeishuConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND app_id = ? AND open_id = ?", old.UserID, old.AppID, old.OpenID).First(&current).Error; err != nil {
			return err
		}
		if current.Tokens != old.Tokens {
			return nil
		}
		var epoch types.FeishuPersonalGrantEpoch
		if err := tx.Where("user_id = ?", old.UserID).First(&epoch).Error; err != nil {
			return err
		}
		_, h, err := loadFeishuHealth(tx, old.UserID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if h == nil || h.Epoch != epoch.Epoch {
			h = &feishuConnectionHealth{AppID: old.AppID, OpenID: old.OpenID, Epoch: epoch.Epoch, ConsentAt: time.Now()}
		}
		h.State, h.ErrorCategory = "reauthorize", feishuRemoteFailureCategory(cause)
		h.CheckedAt, h.CheckAfter = time.Now(), time.Now().Add(5*time.Minute)
		h.Lease = ""
		h.LeaseUntil = time.Time{}
		return storeFeishuHealth(tx, old.UserID, h)
	})
}

// HandleRevocationHint treats events without token fingerprints as authenticated hints,
// never permission to delete the connection. Verify its current grant remotely.
func (s *FeishuPersonalService) HandleRevocationHint(ctx context.Context, eventID, appID, tenantKey, openID string, created time.Time, tokenType string) error {
	if !feishuHealthEnabled() {
		return errors.New("feishu_connection_monitor_disabled")
	}
	cfg, err := s.app(ctx)
	if err != nil || cfg.AppID != appID || !cfg.modeEnabled(types.FeishuUserOAuth) {
		return errors.New("feishu_event_identity_mismatch")
	}
	if eventID == "" || len(eventID) > 256 || tenantKey == "" || openID == "" || created.After(time.Now().Add(time.Minute)) || created.Before(time.Now().Add(-48*time.Hour)) {
		return errors.New("feishu_event_invalid")
	}
	if tokenType != "user_access_token" && tokenType != "refresh_token" && tokenType != "user_access_token refresh_token" {
		return errors.New("feishu_event_invalid")
	}
	var rows []types.FeishuConnection
	if err = s.db.WithContext(ctx).Where("app_id = ? AND open_id = ?", appID, openID).Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, conn := range rows {
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current types.FeishuConnection
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND app_id = ? AND open_id = ?", conn.UserID, appID, openID).First(&current).Error; e != nil {
				return e
			}
			var epoch types.FeishuPersonalGrantEpoch
			if e := tx.Where("user_id = ?", conn.UserID).First(&epoch).Error; e != nil {
				return e
			}
			_, h, e := loadFeishuHealth(tx, conn.UserID)
			if e != nil {
				return e
			}
			if h.Epoch != epoch.Epoch || h.AppID != appID || h.OpenID != openID || h.TenantKey != tenantKey || created.Before(h.ConsentAt) {
				return nil
			}
			hash := sha256.Sum256([]byte(appID + "\x00" + tenantKey + "\x00" + eventID + "\x00" + conn.UserID))
			key := "integration.feishu.revoke_event." + hex.EncodeToString(hash[:])
			receipt, sealErr := sealFeishu(map[string]string{"event_digest": hex.EncodeToString(hash[:]), "kind": tokenType, "user_digest": feishuHealthKey(conn.UserID), "received_at": time.Now().UTC().Format(time.RFC3339Nano)})
			if sealErr != nil {
				return sealErr
			}
			value, _ := json.Marshal(receipt)
			record := types.SystemSetting{Key: key, Value: value, ValueType: "string", Category: "integration", IsSecret: true, Description: "Verified revocation hint receipt; no credentials or source data", LastModifiedBy: conn.UserID}
			saved := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
			if saved.Error != nil {
				return saved.Error
			}
			if saved.RowsAffected == 0 {
				return nil
			}
			h.CheckAfter = time.Time{}
			return storeFeishuHealth(tx, conn.UserID, h)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
