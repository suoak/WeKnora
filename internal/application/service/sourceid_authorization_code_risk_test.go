package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// SID's confirmed 90s code lifetime is distinct from KnowHub's pre-authorize
// transaction lifetime. An unexpired pre-disable code remains redeemable:
// this test documents the residual risk rather than fabricating a live check.
func TestSourceIDPromptLoginPreDisableCodeExpiryAndSingleConsumption(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	for _, scenario := range []string{"pre_disable_unexpired", "expired", "already_consumed"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, info := sourceIDLocalFixture(t)
			s.tenantService = sourceIDLocalTestTenants{}
			cfg := s.config.OIDCAuth
			cfg.SourceIDAdmissionMode = "authorization"
			cfg.SourceIDAuthorizationRecheckConfirmed = true // Synthetic approval only.
			cfg.SourceIDForceReauthPromptLogin = true
			cfg.SourceIDLocalRefreshEnable = false
			cfg.SourceIDLocalAccessTTL = "10m"
			cfg.SourceIDLocalAbsoluteTTL = "10m"
			cfg.SourceIDLocalIdleTTL = "10m"
			cfg.SourceIDAdmissionMaxAge = "10m"
			s.employeeAdmission = nil
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			keys := jwksServer(t, key, "code-risk-key")
			defer keys.Close()
			enabled := true
			issuedAt := time.Time{}
			authenticatedAt := time.Time{}
			used := false
			calls, accepted, freshAuthDenials := 0, 0, 0
			nonce := ""
			code, err := generateRandomString(24)
			require.NoError(t, err)
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/authorize":
					require.Equal(t, []string{"login"}, r.URL.Query()["prompt"])
					if !enabled {
						freshAuthDenials++
						w.WriteHeader(http.StatusForbidden)
						return
					}
					issuedAt, authenticatedAt = time.Now(), time.Now()
					nonce = r.URL.Query().Get("nonce")
					w.WriteHeader(200)
				case "/token":
					calls++
					_ = r.ParseForm()
					// Account disable is intentionally NOT checked here: this is
					// the SID contract, not a proposed new KnowHub assumption.
					if r.PostForm.Get("code") != code || used || time.Since(issuedAt) >= 90*time.Second {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
						return
					}
					used = true
					accepted++
					claims := baseClaims(cfg.IssuerURL, "knowhub")
					claims["nonce"], claims["auth_time"] = nonce, authenticatedAt.Unix()
					_ = json.NewEncoder(w).Encode(oidcTokenResponse{AccessToken: "synthetic-access", IDToken: signedIDToken(t, key, "code-risk-key", claims)})
				case "/userinfo":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": "user-123", "attributes": info.Claims["attributes"]})
				}
			}))
			defer mock.Close()
			cfg.ClientID, cfg.ClientSecret, cfg.ClientAuthMethod = "knowhub", "synthetic-secret", "client_secret_basic"
			cfg.RedirectURI = approvedSourceIDProbeCallbackURI
			cfg.AuthorizationEndpoint, cfg.TokenEndpoint = mock.URL+"/authorize", mock.URL+"/token"
			cfg.UserInfoEndpoint, cfg.JwksURI = mock.URL+"/userinfo", keys.URL
			auth, err := s.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
			require.NoError(t, err)
			var transaction types.OIDCTransaction
			require.NoError(t, db.First(&transaction).Error)
			require.Greater(t, time.Until(transaction.ExpiresAt), 90*time.Second)
			r, err := http.Get(auth.AuthorizationURL)
			require.NoError(t, err)
			r.Body.Close()
			require.Equal(t, 200, r.StatusCode)
			enabled = false
			if scenario == "expired" {
				issuedAt = time.Now().Add(-91 * time.Second) // Mock clock only.
			}
			if scenario == "already_consumed" {
				used = true // Another simulated consumer, never a real code.
			}
			ctx, err := s.PrepareOIDCCallback(context.Background(), auth.State)
			require.NoError(t, err)
			response, loginErr := s.LoginWithOIDC(ctx, code, cfg.RedirectURI, types.TenantProvisioningTenantless)
			require.Equal(t, 1, calls) // No retry even on invalid_grant.
			var sessions int64
			require.NoError(t, db.Model(&types.SourceIDSession{}).Count(&sessions).Error)
			if scenario == "pre_disable_unexpired" {
				require.NoError(t, loginErr)
				require.NotNil(t, response)
				require.Equal(t, 1, accepted)
				require.EqualValues(t, 1, sessions)
				var session types.SourceIDSession
				require.NoError(t, db.First(&session).Error)
				require.LessOrEqual(t, time.Until(session.ExpiresAt), 10*time.Minute)
				_, _, err = s.RefreshToken(context.Background(), response.RefreshToken)
				require.Error(t, err)
			} else {
				require.Error(t, loginErr)
				require.Nil(t, response)
				require.Zero(t, accepted)
				require.Zero(t, sessions)
			}
			_, err = s.PrepareOIDCCallback(context.Background(), auth.State)
			require.Error(t, err)
			require.Equal(t, 1, calls) // Replayed callback cannot issue a second request.
			// Even with a previously valid code, a new forced authentication
			// must reject this now-disabled employee.
			u, err := url.Parse(auth.AuthorizationURL)
			require.NoError(t, err)
			r, err = http.Get(u.String())
			require.NoError(t, err)
			r.Body.Close()
			require.Equal(t, 403, r.StatusCode)
			require.Equal(t, 1, freshAuthDenials)
		})
	}
}
