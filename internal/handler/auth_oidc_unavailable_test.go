package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOIDCProviderUnavailableDoesNotRedirectOrIssueNonce(t *testing.T) {
	h := &AuthHandler{userService: &stubOIDCStartUserService{
		getOIDCAuthorizationURL: func(context.Context, string) (*types.OIDCAuthURLResponse, error) {
			return nil, fmt.Errorf("provider: %w", config.ErrOIDCProviderUnavailable)
		},
	}}
	router := newOIDCStartTestRouter(h)
	router.GET("/auth/oidc/url", h.GetOIDCAuthorizationURL)
	for _, path := range []string{"/auth/oidc/start", "/auth/oidc/url"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
		require.Empty(t, w.Header().Get("Location"))
		require.Empty(t, w.Result().Cookies(), "no authorization transaction may be advertised")
	}
}

func TestOIDCConfigRechecksExpiryWithoutExposingApproval(t *testing.T) {
	provider := &config.OIDCAuthConfig{
		Enable: true, SourceIDEnable: true, SourceIDContractConfirmed: true,
		SourceIDValidationStrategy: config.SourceIDTLSUserInfoValidation,
		IssuerURL:                  config.SourceIDPinnedIssuer, RedirectURI: config.SourceIDHTTPCallback,
		ClientID: "synthetic-client", ClientSecret: "synthetic-secret-must-stay-private",
		ClientAuthMethod: "client_secret_basic", SourceIDClaimsSource: "userinfo",
		DiscoveryURL:                 config.SourceIDPinnedIssuer + "/oidc/.well-known/openid-configuration",
		SourceIDTLSUserInfoConfirmed: true, SourceIDTLSUserInfoApprovalRef: "synthetic-private-approval",
	}
	h := &AuthHandler{configInfo: &config.Config{OIDCAuth: provider}}
	router := gin.New()
	router.GET("/auth/oidc/config", h.GetOIDCConfig)
	for _, expired := range []bool{false, true, false} {
		deadline := time.Now().Add(time.Hour)
		if expired {
			deadline = time.Now().Add(-time.Hour)
		}
		provider.SourceIDTLSUserInfoExpiresAt = deadline.Format(time.RFC3339)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oidc/config", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var result types.OIDCConfigResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, !expired, result.Enabled)
		require.Equal(t, expired, result.Unavailable)
		require.True(t, provider.Enable, "availability reporting must not mutate policy")
		require.NotContains(t, w.Body.String(), provider.ClientSecret)
		require.NotContains(t, w.Body.String(), provider.SourceIDTLSUserInfoApprovalRef)
	}
}
