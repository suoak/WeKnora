package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSourceIDMockLoginAndSessionLifecycle(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mock.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuthToken{}, &types.ExternalIdentity{}, &types.SourceIDEnrollment{}, &types.SourceIDSession{}, &types.SourceIDLogout{}, &types.OIDCTransaction{}))
	user := &types.User{ID: uuid.NewString(), Username: "historical", Email: "original@example.test", IsActive: true}
	require.NoError(t, apprepo.NewUserRepository(db).CreateUser(context.Background(), user))
	require.NoError(t, db.Create(&types.SourceIDEnrollment{ID: uuid.NewString(), Issuer: "https://mock.example", EmployeeNo: "0001", UserID: user.ID, ReviewedBy: "reviewer", ReviewReference: "mock-ticket"}).Error)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys := jwksServer(t, key, "mock-key")
	defer keys.Close()
	nonce := ""
	mode := "valid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		require.NotEmpty(t, r.Form.Get("code_verifier"))
		claims := baseClaims("https://mock.example", "knowhub")
		claims["nonce"] = nonce
		claims["employee"] = "0001"
		claims["session"] = "mock-tgt"
		claims["status"] = "enabled"
		switch mode {
		case "nonce":
			claims["nonce"] = "wrong"
		case "nonce-padding":
			claims["nonce"] = nonce + " "
		case "employee":
			delete(claims, "employee")
		case "inactive":
			claims["status"] = "disabled"
		case "issuer":
			claims["iss"] = "wrong"
		case "audience":
			claims["aud"] = "wrong"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Hour).Unix()
		}
		_ = json.NewEncoder(w).Encode(oidcTokenResponse{AccessToken: "mock-access", IDToken: signedIDToken(t, key, "mock-key", claims)})
	}))
	defer server.Close()
	cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDContractConfirmed: true, SourceIDLogoutEnable: true, PKCEEnable: true, IssuerURL: "https://mock.example", ClientID: "knowhub", ClientSecret: "mock-secret", RedirectURI: "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback", AuthorizationEndpoint: server.URL, TokenEndpoint: server.URL, JwksURI: keys.URL, SourceIDEmployeeClaim: "employee", SourceIDSessionClaim: "session", SourceIDStatusClaim: "status", SourceIDAllowedStatus: "enabled", Scopes: []string{"openid"}}
	s := &userService{config: &config.Config{OIDCAuth: cfg}, userRepo: apprepo.NewUserRepository(db), tokenRepo: apprepo.NewAuthTokenRepository(db)}
	ctx := context.Background()
	login := func() (*types.OIDCCallbackResponse, error) {
		auth, err := s.GetOIDCAuthorizationURL(ctx, cfg.RedirectURI)
		require.NoError(t, err)
		nonce = auth.Nonce
		require.Contains(t, auth.AuthorizationURL, "code_challenge_method=S256")
		require.Contains(t, auth.AuthorizationURL, "nonce=")
		prepared, err := s.PrepareOIDCCallback(ctx, auth.State)
		require.NoError(t, err)
		_, err = s.PrepareOIDCCallback(ctx, auth.State)
		require.Error(t, err)
		return s.LoginWithOIDC(prepared, "mock-code", cfg.RedirectURI, types.TenantProvisioningTenantless)
	}
	for _, failure := range []string{"nonce", "nonce-padding", "employee", "inactive", "issuer", "audience", "expired"} {
		t.Run(failure, func(t *testing.T) { mode = failure; _, err := login(); require.Error(t, err) })
	}
	mode = "valid"
	response, err := login()
	require.NoError(t, err)
	require.Equal(t, user.ID, response.User.ID)
	_, _, err = s.ValidateToken(ctx, response.Token)
	require.NoError(t, err)
	access, refresh, err := s.RefreshToken(ctx, response.RefreshToken)
	require.NoError(t, err)
	_, _, err = s.RefreshToken(ctx, response.RefreshToken)
	require.Error(t, err)
	record, err := s.tokenRepo.GetTokenByValue(ctx, access)
	require.NoError(t, err)
	require.NotEmpty(t, record.SessionID)
	// Tenant reissue uses the authenticated session even without a refresh.
	switchCtx, err := s.SourceIDSwitchContext(ctx, access)
	require.NoError(t, err)
	switched, _, err := s.generateTokensForTenant(switchCtx, user, 42)
	require.NoError(t, err)
	require.NoError(t, s.SourceIDBackchannelLogout(ctx, cfg.IssuerURL, "mock-tgt"))
	for _, token := range []string{response.Token, access, switched} {
		_, _, err := s.ValidateToken(ctx, token)
		require.Error(t, err)
	}
	_, _, err = s.RefreshToken(ctx, refresh)
	require.Error(t, err)
	_, _, err = s.generateTokensForTenant(switchCtx, user, 42)
	require.Error(t, err)
	_, err = login()
	require.Error(t, err) // late callback cannot undo logout
}

func TestSourceIDUserInfoSubjectAndClaimSource(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys := jwksServer(t, key, "kid")
	defer keys.Close()
	sub := "user-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer mock", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": sub, "employee": "002", "active": true})
	}))
	defer server.Close()
	cfg := oidcVerifyCfg(keys.URL)
	cfg.SourceIDEnable = true
	cfg.UserInfoEndpoint = server.URL
	cfg.SourceIDClaimsSource = "userinfo"
	claims := baseClaims(cfg.IssuerURL, cfg.ClientID)
	claims["nonce"] = "expected"
	claims["employee"] = "unselected"
	s := &userService{}
	ctx := context.WithValue(context.Background(), oidcNonceKey, "expected")
	info, err := s.resolveOIDCUserInfo(ctx, cfg, &oidcTokenResponse{AccessToken: "mock", IDToken: signedIDToken(t, key, "kid", claims)})
	require.NoError(t, err)
	require.Equal(t, "002", sourceIDClaim(info.Claims, "employee"))
	sub = "other"
	_, err = s.resolveOIDCUserInfo(ctx, cfg, &oidcTokenResponse{AccessToken: "mock", IDToken: signedIDToken(t, key, "kid", claims)})
	require.Error(t, err)
	sub = " user-123 "
	_, err = s.resolveOIDCUserInfo(ctx, cfg, &oidcTokenResponse{AccessToken: "mock", IDToken: signedIDToken(t, key, "kid", claims)})
	require.Error(t, err)
	require.Empty(t, sourceIDClaim(map[string]interface{}{"attributes": []interface{}{"GH"}}, "attributes.GH"))
	require.Empty(t, sourceIDClaim(map[string]interface{}{"GH": 123}, "GH"))
	require.Empty(t, sourceIDClaim(map[string]interface{}{"GH": strings.Repeat("x", 513)}, "GH"))
}
