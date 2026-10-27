package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	feishuAppSetting = "integration.feishu.personal_app"
	feishuScopes     = "docx:document:readonly docs:document:export " +
		"docs:document.media:download drive:drive.metadata:readonly " +
		"drive:file:readonly wiki:wiki:readonly sheets:spreadsheet:readonly " +
		"bitable:app:readonly offline_access"
)

// ErrFeishuReconnect reports feishu reconnect.
var ErrFeishuReconnect = errors.New("feishu_reconnect_required")

// Refresh failures do not imply that an otherwise readable document forbids export.
var (
	ErrFeishuRefreshExpired     = fmt.Errorf("%w: refresh_token_expired", ErrFeishuReconnect)
	ErrFeishuRefreshRejected    = fmt.Errorf("%w: refresh_rejected", ErrFeishuReconnect)
	ErrFeishuOAuthConfiguration = errors.New("feishu_oauth_application_configuration")
)

// FeishuAppConfig defines feishu app config state.
// A compound encrypted setting avoids partially applied application credentials.
// It deliberately bypasses the general tunable registry: only this dedicated
// admin API can write it, and every read hits the DB (no stale credential cache).
type FeishuAppConfig struct {
	Capabilities       *types.FeishuAccessCapabilities `json:"capabilities,omitempty"`
	ApplicationVersion string                          `json:"application_version,omitempty"`
	PersonalVersion    string                          `json:"personal_version,omitempty"`
	AuthMode           string                          `json:"auth_mode"`
	AppID              string                          `json:"app_id"`
	Secret             string                          `json:"client_secret"`
	RedirectURI        string                          `json:"redirect_uri"`
	Enabled            bool                            `json:"enabled"`
	Version            string                          `json:"version"`
}

// FeishuPersonalService defines feishu personal service state.
type FeishuPersonalService struct {
	dataSources interfaces.DataSourceService
	recovery    *PersonalRecovery
	db          *gorm.DB
	settings    interfaces.SystemSettingRepository
	knowledge   interfaces.KnowledgeService
	kbs         interfaces.KnowledgeBaseService
	audit       interfaces.AuditLogService
	api         *http.Client
	newClient   func(*core.Config) *core.Client
}

func (s *FeishuPersonalService) client(cfg *core.Config) *core.Client {
	if s.newClient != nil {
		return s.newClient(cfg)
	}
	return core.NewClient(cfg)
}

// NewFeishuPersonalService constructs FeishuPersonalService.
func NewFeishuPersonalService(
	db *gorm.DB,
	settings interfaces.SystemSettingRepository,
	knowledge interfaces.KnowledgeService,
	kbs interfaces.KnowledgeBaseService,
	audit interfaces.AuditLogService,
) *FeishuPersonalService {
	return &FeishuPersonalService{
		db:        db,
		settings:  settings,
		knowledge: knowledge,
		kbs:       kbs,
		audit:     audit,
		api: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func sealFeishu(v any) (string, error) {
	key := utils.GetAESKey()
	if len(key) != 32 {
		return "", errors.New("system_aes_key must contain exactly 32 bytes")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return utils.EncryptAESGCM(string(b), key)
}

func openFeishu(ciphertext string, v any) error {
	if !strings.HasPrefix(ciphertext, utils.EncPrefix) {
		return errors.New("unencrypted Feishu credential rejected")
	}
	b, err := utils.DecryptStoredSecret(ciphertext)
	if err != nil {
		return errors.New("feishu credential decryption failed")
	}
	return json.Unmarshal([]byte(b), v)
}

func (s *FeishuPersonalService) app(ctx context.Context) (*FeishuAppConfig, error) {
	row, err := s.settings.Get(ctx, feishuAppSetting)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("feishu_app_not_configured")
	}
	ciphertext, err := row.AsString()
	if err != nil {
		return nil, err
	}
	var cfg FeishuAppConfig
	if err := openFeishu(ciphertext, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateFeishuApp(cfg *FeishuAppConfig) error {
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.Path != "/feishu/callback" {
		return errors.New("callback must be an absolute /feishu/callback URL without query or " +
			"fragment")
	}
	if u.Scheme != "https" &&
		(u.Scheme != "http" || u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		if !feishuTestHTTPCallbackAllowed(cfg) && !feishuInternalHTTPCallbackAllowed(cfg) {
			return errors.New(
				"callback requires HTTPS or an explicitly approved exact HTTP callback policy",
			)
		}
	}
	if !strings.HasPrefix(cfg.AppID, "cli_") || len(cfg.AppID) > 128 || cfg.Secret == "" ||
		len(cfg.Secret) > 1024 {
		return errors.New("invalid application credentials")
	}
	return nil
}

// SaveApp provides the save app operation.
func (s *FeishuPersonalService) SaveApp(ctx context.Context, cfg *FeishuAppConfig) error {
	if !types.IsSystemAdminFromContext(ctx) {
		return errors.New("system_admin_required")
	}
	old, oldErr := s.app(ctx)
	if oldErr == nil && old.Capabilities != nil && cfg.Capabilities == nil {
		return errors.New("feishu_v2_configuration_required")
	}
	if cfg.Secret == "" {
		old, err := s.app(ctx)
		if err != nil || old.AppID != cfg.AppID {
			return errors.New("client_secret_required")
		}
		cfg.Secret = old.Secret
	}
	cfg.AuthMode = types.FeishuAuthMode(cfg.AuthMode)
	if err := validateFeishuModeConfig(cfg); err != nil {
		return err
	}
	if err := verifyFeishuInternalHTTPCallback(ctx, cfg); err != nil {
		return err
	}
	if feishuInternalHTTPCallbackAllowed(cfg) {
		if s.audit == nil {
			return errors.New("feishu_internal_callback_audit_required")
		}
		actor, _ := types.UserIDFromContext(ctx)
		details, err := json.Marshal(map[string]any{
			"approval_ref":    os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_APPROVAL_REF"),
			"callback_digest": stateHash(cfg.RedirectURI), "app_digest": stateHash(cfg.AppID),
			"private_address_digest": stateHash(os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_IPV4")),
		})
		if err != nil {
			return err
		}
		if err := s.audit.Log(ctx, &types.AuditLog{
			ActorUserID: actor, ActorRole: "system_admin",
			Action:     types.AuditAction("feishu.http_callback_policy_verified"),
			TargetType: "system_setting", TargetID: feishuAppSetting,
			Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details),
		}); err != nil {
			return errors.New("feishu_internal_callback_audit_unavailable")
		}
	}
	cfg.Version = uuid.NewString()
	if cfg.Capabilities != nil {
		cfg.ApplicationVersion, cfg.PersonalVersion = uuid.NewString(), uuid.NewString()
		if oldErr == nil && old.AppID == cfg.AppID && old.Secret == cfg.Secret && old.Enabled == cfg.Enabled {
			oldApp := old.modeConfigured(types.FeishuApplicationGrant)
			oldUser := old.modeConfigured(types.FeishuUserOAuth)
			if oldApp == cfg.Capabilities.ApplicationEnabled {
				cfg.ApplicationVersion = old.identityVersion(types.FeishuApplicationGrant)
			}
			if oldUser == cfg.Capabilities.PersonalEnabled && old.RedirectURI == cfg.RedirectURI {
				cfg.PersonalVersion = old.identityVersion(types.FeishuUserOAuth)
			}
		}
	}
	encrypted, err := sealFeishu(cfg)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(encrypted)
	actor, _ := types.UserIDFromContext(ctx)
	if err := s.settings.Upsert(
		ctx,
		&types.SystemSetting{
			Key: feishuAppSetting,
			Value: types.JSON(
				b,
			),
			ValueType:      "string",
			Category:       "integration",
			Description:    "Feishu personal document application (managed by dedicated API)",
			IsSecret:       true,
			LastModifiedBy: actor,
		},
	); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Log(
			ctx,
			&types.AuditLog{
				TenantID:    0,
				ActorUserID: actor,
				ActorRole:   "system_admin",
				Action:      types.AuditActionSystemSettingChanged,
				TargetType:  "system_setting",
				TargetID:    feishuAppSetting,
				Outcome:     types.AuditOutcomeSuccess,
				Details:     types.JSON(`{"credentials_changed":true}`),
			},
		)
	}
	return nil
}

// AdminConfig provides the admin config operation.
func (s *FeishuPersonalService) AdminConfig(ctx context.Context) (map[string]any, error) {
	if !types.IsSystemAdminFromContext(ctx) {
		return nil, errors.New("system_admin_required")
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return map[string]any{
			"configured":   false,
			"v2_available": os.Getenv("FEISHU_ACCESS_V2_ENABLED") == "1",
		}, nil
	}
	return map[string]any{
		"auth_mode":    types.FeishuAuthMode(cfg.AuthMode),
		"capabilities": cfg.Capabilities,
		"v2_available": os.Getenv("FEISHU_ACCESS_V2_ENABLED") == "1",
		"configured":   true,
		"app_id":       cfg.AppID,
		"redirect_uri": cfg.RedirectURI,
		"enabled":      cfg.Enabled,
		"secret_set":   cfg.Secret != "",
	}, nil
}

// TestApp provides the test app operation.
func (s *FeishuPersonalService) TestApp(ctx context.Context) error {
	if !types.IsSystemAdminFromContext(ctx) {
		return errors.New("system_admin_required")
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return err
	}
	token, err := s.client(&core.Config{AppID: cfg.AppID, AppSecret: cfg.Secret}).GetTenantAccessToken(ctx)
	if err != nil || token == "" {
		return errors.New("feishu_application_test_failed")
	}
	return nil // Tests application credentials only, never claims user scopes/ACL.
}

// Status provides the status operation.
func (s *FeishuPersonalService) Status(ctx context.Context) map[string]any {
	cfg, err := s.app(ctx)
	configured := err == nil
	available := configured && cfg.Enabled && len(utils.GetAESKey()) == 32
	mode := types.FeishuUserOAuth
	if configured {
		mode = types.FeishuAuthMode(cfg.AuthMode)
	}
	if mode == types.FeishuApplicationGrant {
		available = available && s.recovery != nil && s.recovery.firstCreateOnly &&
			applicationGrantEnabled() &&
			applicationGrantEngineAvailable() &&
			applicationUserAllowed(types.IsSystemAdminFromContext(ctx))
	}
	connected := false
	if user, ok := types.UserIDFromContext(ctx); ok && available && mode == types.FeishuUserOAuth {
		var c types.FeishuConnection
		connected = s.db.WithContext(ctx).
			Where("user_id = ? AND app_id = ?", user, cfg.AppID).
			First(&c).
			Error == nil &&
			c.RefreshExpiresAt.After(time.Now())
	}
	result := map[string]any{
		"auth_mode":  mode,
		"configured": configured,
		"available":  available,
		"connected":  connected,
	}
	if count, inventoryErr := s.SourceBindingAttention(ctx); inventoryErr == nil {
		result["sources_requiring_rebind"] = count
	} else {
		result["source_binding_status"] = "unavailable"
	}
	if mode == types.FeishuApplicationGrant {
		supported := []string{"docx"}
		if os.Getenv("FEISHU_APPLICATION_TABLES_ENABLED") == "1" {
			supported = append(supported, "sheet", "bitable")
		}
		result["supported_resource_types"] = supported
	}
	if configured && cfg.Capabilities != nil {
		personal := cfg.modeEnabled(types.FeishuUserOAuth) && len(utils.GetAESKey()) == 32
		application := cfg.modeEnabled(types.FeishuApplicationGrant) && s.recovery != nil &&
			s.recovery.firstCreateOnly && applicationGrantEnabled() && applicationGrantEngineAvailable() &&
			applicationUserAllowed(types.IsSystemAdminFromContext(ctx))
		result["capabilities"] = map[string]bool{
			"personal_enabled":    personal,
			"application_enabled": application,
		}
		result["available"] = personal || application
		result["v2"] = true
		result["test_http_callback"] = feishuTestHTTPCallbackAllowed(cfg)
		if feishuTestHTTPCallbackAllowed(cfg) {
			result["test_http_callback_expires_at"] = os.Getenv("FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT")
		}
		if user, ok := types.UserIDFromContext(ctx); ok && personal {
			var c types.FeishuConnection
			result["connected"] = s.db.WithContext(ctx).Where("user_id = ? AND app_id = ?", user, cfg.AppID).
				First(&c).Error == nil && c.RefreshExpiresAt.After(time.Now())
		}
	}
	// An opaque authorization revision allows a timed-out callback to confirm
	// a newly committed grant without exchanging its one-use code again. Token
	// refresh does not change GrantEpoch and cannot masquerade as reauthorization.
	if result["connected"] == true {
		if user, ok := types.UserIDFromContext(ctx); ok && s.db.Migrator().HasTable(&types.FeishuPersonalGrantEpoch{}) {
			var grant types.FeishuPersonalGrantEpoch
			if s.db.WithContext(ctx).Where("user_id = ?", user).First(&grant).Error == nil {
				digest := sha256.Sum256([]byte(user + "\x00" + cfg.AppID + "\x00" + cfg.identityVersion(types.FeishuUserOAuth) + "\x00" + grant.Epoch))
				result["authorization_revision"] = hex.EncodeToString(digest[:])
			}
		}
	}
	if feishuHealthEnabled() && configured && cfg.modeEnabled(types.FeishuUserOAuth) {
		if user, ok := types.UserIDFromContext(ctx); ok {
			h, checkErr := s.inspectConnection(ctx, user)
			result["connected"], result["connection_state"] = false, "disconnected"
			if h != nil && checkErr == nil {
				result["connected"] = h.State == "connected"
				result["connection_state"] = h.State
				result["connection_checked_at"] = h.CheckedAt
			} else if checkErr != nil && !errors.Is(checkErr, ErrFeishuReconnect) {
				result["connection_state"] = "unknown"
			}
		}
	}
	return result
}

func stateHash(state string) string {
	h := sha256.Sum256([]byte(state))
	return hex.EncodeToString(h[:])
}

// Authorize provides the authorize operation.
func (s *FeishuPersonalService) Authorize(ctx context.Context) (string, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return "", errors.New("user_session_required")
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return "", err
	}
	if !cfg.modeEnabled(types.FeishuUserOAuth) {
		return "", ErrFeishuApplicationRestricted
	}
	if !cfg.Enabled || len(utils.GetAESKey()) != 32 {
		return "", errors.New("feishu_app_unavailable")
	}
	if err := verifyFeishuInternalHTTPCallback(ctx, cfg); err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	_ = s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&types.FeishuOAuthState{}).Error
	if err := s.db.WithContext(
		ctx,
	).
		Create(
			&types.FeishuOAuthState{
				Hash: stateHash(
					state,
				),
				UserID:        user,
				ConfigVersion: cfg.identityVersion(types.FeishuUserOAuth),
				ExpiresAt: time.Now().
					Add(
						10 * time.Minute,
					),
			},
		).
		Error; err != nil {
		return "", err
	}
	scopes := feishuScopes
	if cfg.Capabilities != nil {
		// V2 requests its complete read/import scope set at the first consent.
		// This permission inspects the user's effective source rights; it grants
		// neither document editing nor permission to bypass an export denial.
		scopes += " space:document:retrieve docs:permission.member:auth"
	}
	q := url.Values{
		"client_id":     {cfg.AppID},
		"redirect_uri":  {cfg.RedirectURI},
		"response_type": {"code"},
		"scope":         {scopes},
		"state":         {state},
	}
	return "https://accounts.feishu.cn/open-apis/authen/v1/authorize?" + q.Encode(), nil
}

type feishuTokens struct {
	UserID         string `json:"owner"`
	Access         string `json:"access_token"`
	Refresh        string `json:"refresh_token"`
	Expires        int64  `json:"expires_in"`
	RefreshExpires int64  `json:"refresh_token_expires_in"`
	Scope          string `json:"scope"`
	Code           int    `json:"code"`
	Error          string `json:"error"`
}

// No provider-controlled response, URL, or credential is included in errors.
func (s *FeishuPersonalService) oauthRequest(
	ctx context.Context,
	payload map[string]string,
	v2 ...bool,
) (*feishuTokens, error) {
	operation := "exchange_user_code"
	if payload["grant_type"] == "refresh_token" {
		operation = "refresh_user_token"
	}
	observation := core.APIObservation{Operation: operation}
	defer func() { core.RecordAPIObservation(ctx, observation) }()
	b, _ := json.Marshal(payload)
	endpoint := "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	if len(v2) > 0 && v2[0] {
		endpoint = "https://accounts.feishu.cn/oauth/v3/token"
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(b),
	)
	if err != nil {
		return nil, errors.New("feishu_oauth_request_failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.api.Do(req)
	if err != nil {
		observation.Category = core.ErrorCategoryTransient
		return nil, &core.APIError{Category: core.ErrorCategoryTransient, Operation: operation, Cause: err}
	}
	defer func(cleanup func() error) { _ = cleanup() }(resp.Body.Close)
	observation.HTTPStatus = resp.StatusCode
	observation.RequestID = core.ResponseRequestID(resp.Header)
	observation.Category = core.ErrorCategoryPermanent
	if resp.StatusCode == 429 {
		observation.Category = core.ErrorCategoryRateLimit
	} else if resp.StatusCode >= 500 {
		observation.Category = core.ErrorCategoryTransient
	}
	var token feishuTokens
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if readErr != nil {
		observation.Category = core.ErrorCategoryTransient
		return nil, &core.APIError{
			Category: core.ErrorCategoryTransient, Operation: operation,
			HTTPStatus: resp.StatusCode, RequestID: observation.RequestID, Cause: readErr,
		}
	}
	if len(body) > 1024*1024 {
		return nil, core.ErrResponseTooLarge
	}
	if err := json.Unmarshal(body, &token); err != nil {
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			return nil, &core.APIError{
				Category: observation.Category, Operation: operation,
				HTTPStatus: resp.StatusCode, RequestID: observation.RequestID,
			}
		}
		return nil, errors.New("feishu_oauth_invalid_response")
	}
	category := core.ErrorCategory("")
	if resp.StatusCode != 200 || token.Code != 0 || token.Error != "" {
		category = core.ErrorCategoryAuth
	}
	oauthError := ""
	for _, known := range []string{
		"invalid_grant", "invalid_client", "invalid_scope", "invalid_request",
		"temporarily_unavailable", "access_denied",
	} {
		if token.Error == known {
			oauthError = known
		}
	}
	observation.Category, observation.Code, observation.OAuthError = category, token.Code, oauthError
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		kind := core.ErrorCategoryTransient
		if resp.StatusCode == 429 {
			kind = core.ErrorCategoryRateLimit
		}
		observation.Category = kind
		return nil, &core.APIError{
			Category: kind, Operation: operation, HTTPStatus: resp.StatusCode,
			Code: token.Code, RequestID: core.ResponseRequestID(resp.Header),
		}
	}
	if resp.StatusCode != 200 || token.Code != 0 || token.Error != "" || token.Access == "" ||
		token.Refresh == "" ||
		token.Expires <= 0 ||
		token.RefreshExpires <= 0 {
		observation.Category = core.ErrorCategoryAuth
		cause := error(ErrFeishuReconnect)
		if operation == "refresh_user_token" && (oauthError == "invalid_grant" || token.Code == 99991669) {
			cause = ErrFeishuRefreshRejected
		}
		if oauthError == "invalid_client" || oauthError == "invalid_scope" || oauthError == "invalid_request" {
			cause = ErrFeishuOAuthConfiguration
		}
		return nil, errors.Join(cause, &core.APIError{
			Category:  core.ErrorCategoryAuth,
			Operation: operation, HTTPStatus: resp.StatusCode, Code: token.Code,
			RequestID: core.ResponseRequestID(resp.Header),
		})
	}
	return &token, nil
}

// Complete provides the complete operation.
func (s *FeishuPersonalService) Complete(ctx context.Context, state, code string) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok || len(state) != 43 || code == "" || len(code) > 2048 {
		return errors.New("invalid_oauth_callback")
	}
	cfg, err := s.app(ctx)
	if err != nil || !cfg.modeEnabled(types.FeishuUserOAuth) {
		return errors.New("feishu_app_unavailable")
	}
	if !cfg.modeEnabled(types.FeishuUserOAuth) {
		return ErrFeishuApplicationRestricted
	}
	if err := verifyFeishuInternalHTTPCallback(ctx, cfg); err != nil {
		return err
	}

	// Atomic consume is supported by both PostgreSQL and SQLite. Only the owner
	// may consume it; a wrong-user attempt cannot invalidate the owner's state.
	consumed := s.db.WithContext(ctx).
		Model(&types.FeishuOAuthState{}).
		Where("hash = ? AND user_id = ? AND config_version = ? AND expires_at > ? "+
			"AND claimed = ?", stateHash(state), user, cfg.identityVersion(types.FeishuUserOAuth), time.Now(), false).
		Update("claimed", true)
	if consumed.Error != nil {
		return consumed.Error
	}
	if consumed.RowsAffected != 1 {
		return errors.New("invalid_or_expired_oauth_state")
	}
	t, err := s.oauthRequest(
		ctx,
		map[string]string{
			"grant_type":    "authorization_code",
			"client_id":     cfg.AppID,
			"client_secret": cfg.Secret,
			"code":          code,
			"redirect_uri":  cfg.RedirectURI,
		},
		cfg.Capabilities != nil,
	)
	if err != nil {
		return err
	}
	client := s.client(&core.Config{AppID: cfg.AppID})
	identityCtx := core.WithUserIdentity(
		ctx,
		func(context.Context) (string, error) { return t.Access, nil },
	)
	if !cfg.modeEnabled(types.FeishuUserOAuth) {
		return ErrFeishuApplicationRestricted
	}
	var info struct {
		Data struct {
			OpenID    string `json:"open_id"`
			TenantKey string `json:"tenant_key"`
		} `json:"data"`
	}
	if err := client.DoRequest(identityCtx, http.MethodGet, "/open-apis/authen/v1/user_info", nil, &info); err != nil ||
		info.Data.OpenID == "" {
		return errors.New("feishu_identity_verification_failed")
	}
	t.UserID = user
	encrypted, err := sealFeishu(t)
	if err != nil {
		return err
	}
	c := types.FeishuConnection{
		UserID:           user,
		AppID:            cfg.AppID,
		OpenID:           info.Data.OpenID,
		Tokens:           encrypted,
		ExpiresAt:        time.Now().Add(time.Duration(t.Expires) * time.Second),
		RefreshExpiresAt: time.Now().Add(time.Duration(t.RefreshExpires) * time.Second),
	}
	latest, err := s.app(ctx)
	if err != nil ||
		latest.identityVersion(types.FeishuUserOAuth) != cfg.identityVersion(types.FeishuUserOAuth) ||
		!latest.modeEnabled(types.FeishuUserOAuth) {
		return errors.New("feishu_application_changed")
	}
	newEpoch := uuid.NewString()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Disconnect removes the claimed state as well, fencing off a callback
		// already exchanging its code when the user disconnected.
		claim := tx.Where(
			"hash = ? AND user_id = ? AND claimed = ? AND expires_at > ?",
			stateHash(
				state,
			),
			user,
			true,
			time.Now(),
		).
			Delete(&types.FeishuOAuthState{})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return errors.New("invalid_or_expired_oauth_state")
		}
		if err := tx.Where("user_id = ?", user).Delete(&types.FeishuOAuthState{}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(
			clause.OnConflict{
				Columns: []clause.Column{
					{
						Name: "user_id",
					},
				},
				DoUpdates: clause.AssignmentColumns(
					[]string{
						"app_id",
						"open_id",
						"tokens",
						"expires_at",
						"refresh_expires_at",
						"updated_at",
					},
				),
			},
		).
			Create(
				&c,
			).
			Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(&types.FeishuPersonalGrantEpoch{}) {
			if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
				Create(&types.FeishuPersonalGrantEpoch{UserID: user, Epoch: newEpoch}).Error; err != nil {
				return err
			}
			if feishuHealthEnabled() {
				return storeFeishuHealth(tx, user, &feishuConnectionHealth{
					AppID: c.AppID, OpenID: c.OpenID, Epoch: newEpoch, TenantKey: info.Data.TenantKey,
					State: "connected", CheckedAt: time.Now(), ConsentAt: time.Now(), CheckAfter: time.Now().Add(5 * time.Minute),
				})
			}
		}
		return nil
	})
}

// Disconnect provides the disconnect operation.
func (s *FeishuPersonalService) Disconnect(ctx context.Context) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return errors.New("user_session_required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", user).Delete(&types.FeishuConnection{}).Error; err != nil {
			return err
		}
		if err := tx.Where("key = ?", feishuHealthKey(user)).Delete(&types.SystemSetting{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user).Delete(&types.FeishuOAuthState{}).Error; err != nil {
			return err
		}
		return tx.Model(&types.FeishuPersonalImport{}).
			Where("user_id = ? AND auth_mode = ? AND status <> ?", user, types.FeishuUserOAuth, "source_managed").
			Update("status", "needs_attention").
			Error
	})
}

// UserToken is callable by workers with an explicit persisted owner. Every
// request reloads the grant; refreshes lock the row so rotated refresh tokens
// cannot race across replicas. A disconnected grant is never recreated here.
func (s *FeishuPersonalService) UserToken(ctx context.Context, user, appID, openID string) (string, error) {
	if caller, ok := types.UserIDFromContext(ctx); ok && caller != user {
		return "", ErrFeishuReconnect
	}
	if probing, _ := ctx.Value(feishuHealthProbeKey{}).(bool); !probing {
		if err := s.assertConnectionHealth(s.db.WithContext(ctx), user); err != nil {
			return "", err
		}
	}
	var result string
	var failedConnection *types.FeishuConnection
	cfg, err := s.app(ctx)
	if err != nil || !cfg.modeEnabled(types.FeishuUserOAuth) || cfg.AppID != appID {
		return "", ErrFeishuReconnect
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// SQLite omits SELECT FOR UPDATE. Acquire its writer lock before the
		// first read so concurrent replicas cannot rotate one refresh token twice.
		if tx.Name() == "sqlite" {
			lock := tx.Model(&types.FeishuConnection{}).
				Where("user_id = ? AND app_id = ? AND open_id = ?", user, appID, openID).
				UpdateColumn("tokens", gorm.Expr("tokens"))
			if lock.Error != nil {
				return lock.Error
			}
			if lock.RowsAffected != 1 {
				return ErrFeishuReconnect
			}
		}
		var c types.FeishuConnection
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).
			Where(
				"user_id = ? AND app_id = ? AND open_id = ?",
				user,
				appID,
				openID,
			).
			First(
				&c,
			).
			Error; err != nil {
			return ErrFeishuReconnect
		}
		var t feishuTokens
		copyConnection := c
		failedConnection = &copyConnection
		if err := openFeishu(c.Tokens, &t); err != nil || t.UserID != user {
			return ErrFeishuReconnect
		}
		accessHash := sha256.Sum256([]byte(t.Access))
		forceRefresh := core.UserTokenRefreshFingerprint(ctx) == hex.EncodeToString(accessHash[:])
		if c.ExpiresAt.After(time.Now().Add(time.Minute)) && !forceRefresh {
			result = t.Access
			return nil
		}
		if !c.RefreshExpiresAt.After(time.Now()) {
			return ErrFeishuRefreshExpired
		}
		next, err := s.oauthRequest(
			ctx,
			map[string]string{
				"grant_type":    "refresh_token",
				"client_id":     cfg.AppID,
				"client_secret": cfg.Secret,
				"refresh_token": t.Refresh,
			},
			cfg.Capabilities != nil,
		)
		if err != nil {
			return err
		}
		next.UserID = user
		// OAuth refresh may omit an unchanged scope inventory. This preserves
		// evidence from the original grant; a nonempty reduced scope replaces it.
		if next.Scope == "" {
			next.Scope = t.Scope
		}
		c.Tokens, err = sealFeishu(next)
		if err != nil {
			return err
		}
		c.ExpiresAt = time.Now().Add(time.Duration(next.Expires) * time.Second)
		c.RefreshExpiresAt = time.Now().Add(time.Duration(next.RefreshExpires) * time.Second)
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		result = next.Access
		return nil
	})
	if err != nil {
		if probing, _ := ctx.Value(feishuHealthProbeKey{}).(bool); !probing {
			s.noteConnectionFailure(ctx, failedConnection, err)
		}
	}
	return result, err
}

// ListImports provides the list imports operation.
func (s *FeishuPersonalService) ListImports(
	ctx context.Context,
	kbID string,
) ([]types.FeishuPersonalImport, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("user_session_required")
	}
	rows := []types.FeishuPersonalImport{}
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND kb_id = ?", user, kbID).
		Order("updated_at DESC").
		Limit(100).
		Find(&rows).
		Error
	return s.decorateFeishuImports(ctx, rows, err)
}

func (s *FeishuPersonalService) decorateFeishuImports(
	ctx context.Context,
	rows []types.FeishuPersonalImport,
	err error,
) ([]types.FeishuPersonalImport, error) {
	if err == nil && s.knowledge != nil {
		for i := range rows {
			if rows[i].Status == "queued" && rows[i].KnowledgeID != "" {
				if knowledge, loadErr := s.knowledge.GetKnowledgeByID(ctx, rows[i].KnowledgeID); loadErr == nil {
					switch knowledge.ParseStatus {
					case types.ParseStatusCompleted:
						rows[i].Status = "synced"
					case types.ParseStatusFailed:
						rows[i].Status = "needs_attention"
					}
				}
			}
		}
	}

	if err == nil && s.recovery != nil && len(rows) > 0 {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		var resources []types.FeishuPersonalResource
		if err = s.db.WithContext(ctx).Where("import_id IN ?", ids).Find(&resources).Error; err != nil {
			return nil, err
		}
		byID := make(map[string]types.FeishuPersonalResource, len(resources))
		for _, resource := range resources {
			byID[resource.ImportID] = resource
		}
		attemptIDs := make([]string, 0, len(resources))
		for _, resource := range resources {
			attemptIDs = append(attemptIDs, resource.CurrentAttemptID)
		}
		var attempts []types.FeishuPersonalAttempt
		if len(attemptIDs) > 0 {
			if err = s.db.WithContext(
				ctx,
			).
				Select(
					"id",
					"last_error_category",
				).
				Where(
					"id IN ?",
					attemptIDs,
				).
				Find(
					&attempts,
				).
				Error; err != nil {
				return nil, err
			}
		}
		categories := map[string]string{}
		for _, a := range attempts {
			if a.LastErrorCategory != "" {
				categories[a.ID] = personalImportPublicError(a.LastErrorCategory)
			}
		}
		for i := range rows {
			if resource, ok := byID[rows[i].ID]; ok {
				rows[i].LogicalKnowledgeID = resource.LogicalKnowledgeID
				rows[i].AttemptID = resource.CurrentAttemptID
				rows[i].ErrorCategory = categories[resource.CurrentAttemptID]
			}
		}
	}
	if err == nil && s.recovery != nil && s.recovery.firstCreateOnly {
		positions := []int{}
		pending := []types.FeishuPersonalImport{}
		for i, row := range rows {
			if row.Status != "source_managed" {
				positions = append(positions, i)
				pending = append(pending, row)
			} else {
				rows[i].IndexStatus = "managed_by_source"
				rows[i].WikiStatus = "managed_by_source"
			}
		}
		err = s.recovery.firstCreateStatuses(ctx, pending)
		for i, pos := range positions {
			rows[pos] = pending[i]
		}
	}
	if err == nil && s.dataSources != nil && len(rows) > 0 {
		sources, loadErr := s.dataSources.ListDataSources(ctx, rows[0].KBID)
		if loadErr != nil {
			return nil, loadErr
		}
		byID := map[string]*types.DataSource{}
		for _, ds := range sources {
			byID[ds.ID] = ds
		}
		for i := range rows {
			if ds := byID[rows[i].ID]; ds != nil {
				if config, parseErr := ds.ParseConfig(); parseErr == nil {
					if ref, refErr := types.ParseFeishuSourceReference(config); refErr == nil && ref != nil &&
						ref.OwnerUserID == rows[i].UserID {
						if bindingErr := validatePersonalSourceBinding(s.db.WithContext(ctx), ref); errors.Is(
							bindingErr,
							ErrFeishuSourceRebindRequired,
						) {
							rows[i].ErrorCategory = "source_identity_rebind_required"
						}
					}
				}
				rows[i].DataSourceID = ds.ID
				rows[i].SourceStatus = ds.Status
				if ds.LegacyAccessState == "hold" || ds.LegacyAccessState == "revoked" {
					rows[i].SourceStatus = ds.LegacyAccessState
				}
			}
		}
	}

	return rows, err
}

func personalImportPublicError(category string) string {
	switch category {
	case "candidate_media_not_supported",
		"candidate_content_incomplete",
		"authorization_revalidation_failed",
		"authorization_or_resource_missing",
		"partial_or_unknown_index_requires_review",
		"parse_or_index_result_unconfirmed",
		"pg_index_receipt_unconfirmed":
		return category
	default:
		return "operation_failed"
	}
}

func feishuFile(data []byte, name string) (*multipart.Form, *multipart.FileHeader, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	p, err := w.CreateFormFile("file", name)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.Write(data); err != nil {
		return nil, nil, err
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	r := multipart.NewReader(&b, w.Boundary())
	f, err := r.ReadForm(8 * 1024 * 1024)
	if err != nil {
		return nil, nil, err
	}
	return f, f.File["file"][0], nil
}

// Import provides the import operation.
func (s *FeishuPersonalService) Import(
	ctx context.Context,
	kbID, rawURL string,
) (*types.FeishuPersonalImport, error) {
	if s.recovery != nil {
		return s.ImportWithIdentity(ctx, kbID, rawURL, "")
	}
	if cfg, err := s.app(ctx); err == nil &&
		(cfg.Capabilities != nil || !cfg.modeEnabled(types.FeishuUserOAuth)) {
		return nil, ErrFeishuApplicationRestricted
	}
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("user_session_required")
	}
	// Service enforcement complements the router guard, including shared KBs.
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err := access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	resource, err := core.ParsePersonalResourceURL(rawURL)
	if err != nil {
		return nil, err
	}
	cfg, err := s.app(ctx)
	if err != nil || !cfg.modeEnabled(types.FeishuUserOAuth) {
		return nil, errors.New("feishu_app_unavailable")
	}
	var c types.FeishuConnection
	if err := s.db.WithContext(ctx).Where("user_id = ? AND app_id = ?", user, cfg.AppID).First(&c).Error; err != nil {
		return nil, ErrFeishuReconnect
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	remoteCtx = core.WithUserIdentity(remoteCtx, func(callCtx context.Context) (string, error) {
		return s.UserToken(callCtx, user, cfg.AppID, c.OpenID)
	})
	client := s.client(&core.Config{AppID: cfg.AppID})
	resource, err = client.ResolvePersonalResource(remoteCtx, resource)
	if err != nil {
		// Wiki resolution may fail before the underlying type/token is known.
		// Match the saved link as well, so losing access still marks the import
		// for attention without touching its knowledge or another app/user.
		_ = s.db.WithContext(ctx).Model(&types.FeishuPersonalImport{}).
			Where("user_id = ? AND app_id = ? AND kb_id = ? AND status <> ?", user, cfg.AppID, kbID, "syncing").
			Where("(resource_type = ? AND document_token = ?) OR url = ?", resource.Type, resource.Token, resource.URL).
			Update("status", "needs_attention").Error
		return nil, err
	}
	token, sourceURL := resource.Token, resource.URL
	row := types.FeishuPersonalImport{
		ID:            uuid.NewString(),
		UserID:        user,
		AppID:         cfg.AppID,
		OpenID:        c.OpenID,
		KBID:          kbID,
		DocumentToken: token,
		ResourceType:  resource.Type,
		URL:           sourceURL,
		Status:        "pending",
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	// GORM adds a non-zero destination primary key to SELECT predicates. A
	// conflicting insert must load by resource identity, not the attempted UUID.
	row = types.FeishuPersonalImport{}
	if err := s.db.WithContext(ctx).Where("user_id = ? AND app_id = ? AND kb_id = ? AND document_token = ? AND "+
		"resource_type = ?", user, cfg.AppID, kbID, token, resource.Type).First(&row).Error; err != nil {
		return nil, err
	}
	if row.OpenID != c.OpenID {
		return nil, errors.New("feishu_import_identity_changed")
	}
	// A DB lease serializes imports across replicas. Timeout exceeds the remote
	// deadline and protects replacement from simultaneous re-sync requests.
	leaseID := uuid.NewString()
	lease := s.db.WithContext(ctx).
		Model(&types.FeishuPersonalImport{}).
		Where("id = ? AND (status <> ? OR updated_at < ?)", row.ID, "syncing", time.Now().Add(-5*time.Minute)).
		Updates(map[string]any{"status": "syncing", "sync_lease": leaseID, "updated_at": time.Now()})
	if lease.Error != nil {
		return nil, lease.Error
	}
	if lease.RowsAffected != 1 {
		return nil, errors.New("feishu_sync_in_progress")
	}
	status := "needs_attention"
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.db.WithContext(cleanupCtx).
			Model(&types.FeishuPersonalImport{}).
			Where("id = ? AND status = ? AND sync_lease = ?", row.ID, "syncing", leaseID).
			Updates(map[string]any{"status": status, "updated_at": time.Now()}).
			Error
	}()
	// Always verify the current user's ACL before recognizing a duplicate.
	if row.KnowledgeID != "" && row.SourceModified == resource.Modified && resource.Modified != "" {
		if existing, err := s.knowledge.GetKnowledgeByID(ctx, row.KnowledgeID); err == nil &&
			existing.ParseStatus == types.ParseStatusCompleted {
			status = "synced"
			row.Status = status
			return &row, nil
		}
	}
	if row.KnowledgeID != "" {
		return nil, repository.ErrPersonalUpdateDisabled
	}
	data, name, err := client.DownloadPersonalResource(remoteCtx, resource)
	if err != nil {
		return nil, err
	}
	count, err := client.ValidateWorkbookCoverage(remoteCtx, resource, data)
	if err != nil {
		return nil, err
	}
	// Recheck disconnect and identity after the export, before any mutation.
	if _, err := s.UserToken(ctx, user, row.AppID, row.OpenID); err != nil {
		return nil, err
	}
	form, file, err := feishuFile(data, name)
	if err != nil {
		return nil, err
	}
	defer func(cleanup func() error) { _ = cleanup() }(form.RemoveAll)
	coverage := "exported_content; embedded objects and attachments require manual " +
		"completeness review"
	if resource.Type == "file" {
		coverage = "original_file"
	}
	if count > 0 {
		coverage = fmt.Sprintf(
			"verified_table_count=%d; field/record/attachment semantics require "+
				"manual completeness review",
			count,
		)
	}
	metadata := map[string]string{
		"source_url":             sourceURL,
		"original_url":           strings.TrimSpace(rawURL),
		"feishu_document_token":  token,
		"feishu_resource_type":   resource.Type,
		"feishu_resource_id":     core.PersonalResourceID(resource),
		"feishu_imported_by":     user,
		"feishu_open_id":         row.OpenID,
		"feishu_source_modified": resource.Modified,
		"feishu_coverage":        coverage,
		"feishu_synced_at":       time.Now().UTC().Format(time.RFC3339),
	}
	var knowledge *types.Knowledge
	knowledge, err = s.knowledge.CreateKnowledgeFromFile(
		ctx,
		kbID,
		file,
		metadata,
		nil,
		name,
		nil,
		"feishu_personal",
		nil,
	)
	if err != nil {
		var duplicate *types.DuplicateKnowledgeError
		if !errors.As(err, &duplicate) || duplicate.Knowledge == nil {
			return nil, err
		}
		// Never claim another importer's identical file as this user's import.
		if row.KnowledgeID == "" {
			return nil, errors.New("identical_content_already_exists_in_knowledge_base")
		}
		knowledge = duplicate.Knowledge
	}
	row.KnowledgeID,
		row.SourceModified,
		row.Status,
		row.UpdatedAt,
		row.Coverage = knowledge.ID,
		resource.Modified,
		"queued",
		time.Now(),
		coverage
	update := s.db.WithContext(ctx).
		Model(&types.FeishuPersonalImport{}).
		Where("id = ? AND status = ? AND sync_lease = ?", row.ID, "syncing", leaseID).
		Updates(
			map[string]any{
				"knowledge_id": row.KnowledgeID,

				"source_modified": resource.Modified,

				"coverage": coverage,

				"url": sourceURL,

				"status": row.Status,

				"updated_at": row.UpdatedAt,
			},
		)
	if update.Error != nil {
		return nil, update.Error
	}
	if update.RowsAffected != 1 {
		return nil, errors.New("feishu_sync_interrupted")
	}
	status = "queued"
	return &row, nil
}
