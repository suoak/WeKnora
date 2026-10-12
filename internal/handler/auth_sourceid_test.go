package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSourceIDPeerTrustDoesNotUseForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "http://mock/logout", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	secret := strings.Repeat("mock", 8)
	r.Header.Set("X-KnowHub-SourceID-Secret", secret)
	require.True(t, trustedSourceIDPeer(r, "192.0.2.0/24", secret))
	r.Header.Set("X-Forwarded-For", "192.0.2.10")
	r.RemoteAddr = "198.51.100.5:1234"
	require.False(t, trustedSourceIDPeer(r, "192.0.2.0/24", secret))
	require.False(t, trustedSourceIDPeer(r, "", secret))
	require.False(t, trustedSourceIDPeer(r, "198.51.100.0/24,invalid", secret))
	r.Header.Set("X-KnowHub-SourceID-Secret", "wrong")
	require.False(t, trustedSourceIDPeer(r, "198.51.100.0/24", secret))
}

type sourceIDHandlerStub struct {
	interfaces.UserService
	calls          int
	index          string
	resultConsumed bool
}

func (s *sourceIDHandlerStub) PrepareOIDCCallback(ctx context.Context, _ string) (context.Context, error) {
	return ctx, nil
}
func (s *sourceIDHandlerStub) SourceIDSwitchContext(ctx context.Context, _ string) (context.Context, error) {
	return ctx, nil
}
func (s *sourceIDHandlerStub) SourceIDBackchannelLogout(_ context.Context, _, index string) error {
	s.calls++
	s.index = index
	return nil
}
func (s *sourceIDHandlerStub) SaveOIDCLoginResult(context.Context, string) (string, error) {
	return "mock-cookie", nil
}
func (s *sourceIDHandlerStub) ConsumeOIDCLoginResult(_ context.Context, key string) (string, error) {
	if key != "mock-cookie" || s.resultConsumed {
		return "", errors.New("expired")
	}
	s.resultConsumed = true
	return base64.RawURLEncoding.EncodeToString([]byte(`{"success":true,"token":"mock-only"}`)), nil
}

func TestSourceIDBackchannelHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &sourceIDHandlerStub{}
	cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDLogoutEnable: true, SourceIDContractConfirmed: true, IssuerURL: "mock-issuer", SourceIDLogoutSecret: strings.Repeat("mock", 8), SourceIDLogoutCIDRs: "192.0.2.0/24"}
	h := &AuthHandler{userService: stub, configInfo: &config.Config{OIDCAuth: cfg}}
	router := gin.New()
	router.POST("/api/v1/auth/oidc/backchannel-logout", h.SourceIDBackchannelLogout)
	xml := `<p:LogoutRequest xmlns:p="urn:oasis:names:tc:SAML:2.0:protocol" ID="mock-request" Version="2.0" IssueInstant="2026-10-12T00:00:00Z"><p:SessionIndex>mock-index</p:SessionIndex></p:LogoutRequest>`
	for _, tc := range []struct {
		name, body, contentType, secret string
		status                          int
	}{
		{"normal", url.Values{"logoutRequest": {xml}}.Encode(), "application/x-www-form-urlencoded", cfg.SourceIDLogoutSecret, 200},
		{"duplicate notification", url.Values{"logoutRequest": {xml}}.Encode(), "application/x-www-form-urlencoded", cfg.SourceIDLogoutSecret, 200},
		{"unauthorized", url.Values{"logoutRequest": {xml}}.Encode(), "application/x-www-form-urlencoded", "", 403},
		{"wrong type", xml, "application/xml", cfg.SourceIDLogoutSecret, 415},
		{"malformed", url.Values{"logoutRequest": {"broken"}}.Encode(), "application/x-www-form-urlencoded", cfg.SourceIDLogoutSecret, 400},
		{"duplicate form field", url.Values{"logoutRequest": {xml, xml}}.Encode(), "application/x-www-form-urlencoded", cfg.SourceIDLogoutSecret, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/auth/oidc/backchannel-logout", strings.NewReader(tc.body))
			request.RemoteAddr = "192.0.2.5:1234"
			request.Header.Set("Content-Type", tc.contentType)
			request.Header.Set("X-KnowHub-SourceID-Secret", tc.secret)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, tc.status, w.Code)
			require.Empty(t, w.Body.String())
		})
	}
	require.Equal(t, 2, stub.calls)
	require.Equal(t, "mock-index", stub.index)
	cfg.SourceIDLogoutEnable = false
	request := httptest.NewRequest("POST", "/api/v1/auth/oidc/backchannel-logout", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	require.Equal(t, 503, w.Code)
}

func TestOIDCResultRejectsCrossSiteAndConsumesCookie(t *testing.T) {
	stub := &sourceIDHandlerStub{}
	h := &AuthHandler{userService: stub}
	router := gin.New()
	router.POST("/api/v1/auth/oidc/result", h.OIDCLoginResult)
	request := func(origin string, header bool) int {
		r := httptest.NewRequest("POST", "http://knowhub.test/api/v1/auth/oidc/result", nil)
		r.AddCookie(&http.Cookie{Name: "knowhub_oidc_result", Value: "mock-cookie"})
		if header {
			r.Header.Set("X-KnowHub-OIDC-Result", "1")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w.Code
	}
	require.Equal(t, 403, request("http://evil.test", true))
	require.Equal(t, 403, request("", false))
	require.Equal(t, 200, request("http://knowhub.test", true))
	require.Equal(t, 401, request("http://knowhub.test", true))
}

func TestOIDCURLUsesApprovedCallbackWithoutBrowserOverride(t *testing.T) {
	approved := "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"
	var callback string
	stub := &stubOIDCStartUserService{getOIDCAuthorizationURL: func(_ context.Context, uri string) (*types.OIDCAuthURLResponse, error) {
		callback = uri
		return &types.OIDCAuthURLResponse{Success: true, AuthorizationURL: "https://mock.example/authorize", Nonce: "mock-nonce"}, nil
	}}
	h := &AuthHandler{userService: stub, configInfo: &config.Config{OIDCAuth: &config.OIDCAuthConfig{RedirectURI: approved}}}
	router := gin.New()
	router.GET("/api/v1/auth/oidc/url", h.GetOIDCAuthorizationURL)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "http://alternate.test/api/v1/auth/oidc/url", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, approved, callback)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "http://alternate.test/api/v1/auth/oidc/url?redirect_uri=https://evil.test/callback", nil))
	require.Equal(t, 400, w.Code)
}

func TestSourceIDOIDCConfigReturnsPublicCallbackWithoutSecrets(t *testing.T) {
	callback := "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"
	h := &AuthHandler{configInfo: &config.Config{OIDCAuth: &config.OIDCAuthConfig{Enable: true, RedirectURI: callback, ClientSecret: "mock-secret-never-public"}}}
	router := gin.New()
	router.GET("/api/v1/auth/oidc/config", h.GetOIDCConfig)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/oidc/config", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), callback)
	require.NotContains(t, w.Body.String(), "mock-secret-never-public")
}
