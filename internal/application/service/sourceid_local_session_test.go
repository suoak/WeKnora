package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sourceIDLocalTestTenants struct{ interfaces.TenantService }

func (sourceIDLocalTestTenants) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 42, Name: "synthetic-existing-space"}, nil
}

func sourceIDLocalFixture(t *testing.T) (*userService, *gorm.DB, *types.OIDCUserInfo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local.db")), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuthToken{}))
	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
	require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
	for _, file := range []string{"003024_sourceid_oidc", "003026_sourceid_binding_review", "003027_sourceid_local_session"} {
		sql, err := os.ReadFile("../../../migrations/sqlite/" + file + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(sql)).Error)
	}
	require.NoError(t, db.Exec("CREATE TABLE tenant_members(user_id TEXT,tenant_id BIGINT,role TEXT,status TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE organization_tenant_members(tenant_id BIGINT,role TEXT,representative_user_id TEXT)").Error)
	user := &types.User{ID: uuid.NewString(), Username: "synthetic-ordinary", Email: "ordinary@example.test", IsActive: true, TenantID: 42}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members VALUES (?,42,'contributor','active')", user.ID).Error)
	cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDContractConfirmed: true,
		SourceIDSessionMode: "local_approved", SourceIDEmployeeClaim: "attributes.RJGH", SourceIDClaimsSource: "userinfo",
		SourceIDStatusClaim: "mock_eligibility", SourceIDAllowedStatus: "ALLOW", SourceIDEmailBindingEnable: true,
		SourceIDEmailContractConfirmed: true, SourceIDAutoBindingEnable: true, SourceIDAutoBindingPolicyRef: "synthetic-binding-approval",
		SourceIDLocalPolicyRef: "synthetic-local-approval", SourceIDAdmissionPolicyRef: "synthetic-sid-status-contract",
		SourceIDAdmissionMode: "userinfo_claim", SourceIDLocalAccessTTL: "10m", SourceIDLocalAbsoluteTTL: "8h",
		SourceIDLocalIdleTTL: "30m", SourceIDAdmissionMaxAge: "1h", SourceIDLocalRefreshEnable: true, IssuerURL: "https://mock.example"}
	svc := &userService{config: &config.Config{OIDCAuth: cfg}, userRepo: apprepo.NewUserRepository(db), tokenRepo: apprepo.NewAuthTokenRepository(db)}
	info := &types.OIDCUserInfo{Subject: "synthetic-sub", Claims: map[string]interface{}{
		"mock_eligibility": "ALLOW", "attributes": map[string]interface{}{"RJGH": "synthetic-001", "RJEMAIL": "ordinary@example.test"},
	}}
	return svc, db, info
}

func TestSourceIDLocalSessionBindingAndTokenLifecycle(t *testing.T) {
	s, db, info := sourceIDLocalFixture(t)
	ctx := context.Background()
	user, first, err := s.loginSourceID(ctx, s.config.OIDCAuth, info)
	require.NoError(t, err)
	require.EqualValues(t, 42, user.TenantID)
	access, refresh, err := s.generateTokensForTenant(first, user, 42)
	require.NoError(t, err)
	validated, tenant, err := s.ValidateToken(ctx, access)
	require.NoError(t, err)
	require.Equal(t, user.ID, validated.ID)
	require.EqualValues(t, 42, tenant)
	info.Claims["attributes"].(map[string]interface{})["RJEMAIL"] = "changed@example.test"
	other, second, err := s.loginSourceID(ctx, s.config.OIDCAuth, info)
	require.NoError(t, err)
	require.Equal(t, user.ID, other.ID)
	require.NotEqual(t, first.Value(sourceIDSessionKey), second.Value(sourceIDSessionKey))
	device2, _, err := s.generateTokensForTenant(second, other, 42)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var base types.SourceIDSession
	require.NoError(t, db.First(&base, "id = ?", first.Value(sourceIDSessionKey)).Error)
	require.Empty(t, base.SessionIndexHash)
	row, err := s.tokenRepo.GetTokenByValue(ctx, access)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), row.ExpiresAt, 2*time.Second)
	nextAccess, _, err := s.RefreshToken(ctx, refresh)
	require.NoError(t, err)
	_, _, err = s.RefreshToken(ctx, refresh)
	require.Error(t, err) // replay revokes this device family
	_, _, err = s.ValidateToken(ctx, nextAccess)
	require.Error(t, err)
	_, _, err = s.ValidateToken(ctx, device2)
	require.NoError(t, err)
	require.NoError(t, s.Logout(ctx, device2))
	_, _, err = s.ValidateToken(ctx, device2)
	require.Error(t, err)
}

func TestSourceIDLocalSessionAdmissionAndExpiryFailClosed(t *testing.T) {
	for _, scenario := range []string{"DENY", "UNKNOWN", "protocol_only", "unapproved", "idle", "admission_expired", "absolute", "refresh_disabled", "binding_revoke", "admin_revoke", "missing_metadata"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, info := sourceIDLocalFixture(t)
			cfg := s.config.OIDCAuth
			ctx := context.Background()
			if scenario == "DENY" {
				info.Claims["mock_eligibility"] = "DENY"
			}
			if scenario == "UNKNOWN" {
				delete(info.Claims, "mock_eligibility")
			}
			if scenario == "protocol_only" {
				cfg.SourceIDProtocolOnly = true
			}
			if scenario == "unapproved" {
				cfg.SourceIDLocalPolicyRef = ""
			}
			if scenario == "refresh_disabled" {
				cfg.SourceIDLocalRefreshEnable = false
			}
			user, loginCtx, err := s.loginSourceID(ctx, cfg, info)
			if scenario == "DENY" || scenario == "UNKNOWN" || scenario == "protocol_only" || scenario == "unapproved" {
				require.Error(t, err)
				var count int64
				require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&count).Error)
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			access, refresh, err := s.generateTokensForTenant(loginCtx, user, 42)
			require.NoError(t, err)
			id := loginCtx.Value(sourceIDSessionKey).(string)
			switch scenario {
			case "idle":
				require.NoError(t, db.Model(&types.SourceIDLocalSession{}).Where("id = ?", id).Update("last_seen_at", time.Now().Add(-31*time.Minute)).Error)
			case "admission_expired":
				require.NoError(t, db.Model(&types.SourceIDLocalSession{}).Where("id = ?", id).Update("admission_valid_until", time.Now().Add(-time.Second)).Error)
			case "absolute":
				require.NoError(t, db.Model(&types.SourceIDSession{}).Where("id = ?", id).Update("expires_at", time.Now().Add(-time.Second)).Error)
			case "refresh_disabled":
				_, _, err = s.RefreshToken(ctx, refresh)
				require.Error(t, err)
				return
			case "binding_revoke":
				admin := s.userRepo.(interface {
					ReviewSourceIDBinding(context.Context, string, string, string, string) error
				})
				var review types.SourceIDBindingReview
				require.NoError(t, db.First(&review).Error)
				require.NoError(t, admin.ReviewSourceIDBinding(ctx, review.ID, "synthetic-admin", "synthetic-revoke", "revoke"))
			case "admin_revoke":
				store, err := s.sourceIDStore()
				require.NoError(t, err)
				require.NoError(t, store.RevokeSourceIDSession(ctx, id, user.ID))
			case "missing_metadata":
				require.NoError(t, db.Delete(&types.SourceIDLocalSession{}, "id = ?", id).Error)
			}
			_, _, err = s.ValidateToken(ctx, access)
			require.Error(t, err)
			_, _, err = s.RefreshToken(ctx, refresh)
			require.Error(t, err)
		})
	}
}

func TestSourceIDLocalSessionRealOIDCServiceWithMockSID(t *testing.T) {
	s, db, info := sourceIDLocalFixture(t)
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	s.tenantService = sourceIDLocalTestTenants{}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys := jwksServer(t, key, "mock-local")
	defer keys.Close()
	cfg := s.config.OIDCAuth
	nonce := ""
	exchanges := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/userinfo" {
			require.Equal(t, "Bearer synthetic-access", r.Header.Get("Authorization"))
			claims := map[string]interface{}{"sub": "user-123", "mock_eligibility": "ALLOW", "attributes": info.Claims["attributes"]}
			_ = json.NewEncoder(w).Encode(claims)
			return
		}
		exchanges++
		client, secret, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "knowhub", client)
		require.Equal(t, "synthetic-secret", secret)
		require.NoError(t, r.ParseForm())
		require.Empty(t, r.Form.Get("code_verifier"))
		require.Empty(t, r.Form.Get("client_secret"))
		claims := baseClaims(cfg.IssuerURL, cfg.ClientID)
		claims["nonce"] = nonce
		_ = json.NewEncoder(w).Encode(oidcTokenResponse{AccessToken: "synthetic-access", IDToken: signedIDToken(t, key, "mock-local", claims)})
	}))
	defer mock.Close()
	cfg.ClientID = "knowhub"
	cfg.ClientSecret = "synthetic-secret"
	cfg.ClientAuthMethod = "client_secret_basic"
	cfg.RedirectURI = "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"
	cfg.AuthorizationEndpoint = mock.URL + "/authorize"
	cfg.TokenEndpoint = mock.URL + "/token"
	cfg.UserInfoEndpoint = mock.URL + "/userinfo"
	cfg.UserInfoTransport = "bearer_get"
	cfg.JwksURI = keys.URL
	cfg.Scopes = []string{"openid"}
	auth, err := s.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
	require.NoError(t, err)
	nonce = auth.Nonce
	require.NotContains(t, auth.AuthorizationURL, "code_challenge")
	prepared, err := s.PrepareOIDCCallback(context.Background(), auth.State)
	require.NoError(t, err)
	response, err := s.LoginWithOIDC(prepared, "synthetic-fresh-code", cfg.RedirectURI, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.True(t, response.Success)
	require.NotEmpty(t, response.Token)
	require.Equal(t, 1, exchanges)
	user, _, err := s.ValidateToken(context.Background(), response.Token)
	require.NoError(t, err)
	require.Equal(t, response.User.ID, user.ID)
	var users int64
	require.NoError(t, db.Model(&types.User{}).Count(&users).Error)
	require.EqualValues(t, 1, users)
}
