package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceIDPromptLoginURLAndDurableTransaction(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	for _, scenario := range []string{
		"sourceid",
		"other_provider",
		"default_off",
		"duplicate_endpoint_prompt",
		"unsupported_max_age",
	} {
		t.Run(scenario, func(t *testing.T) {
			s, db, _ := sourceIDLocalFixture(t)
			cfg := s.config.OIDCAuth
			cfg.AuthorizationEndpoint = "https://127.0.0.1:18443/authorize"
			cfg.TokenEndpoint = "https://127.0.0.1:18443/token"
			cfg.JwksURI = "https://127.0.0.1:18443/jwks"
			cfg.RedirectURI = approvedSourceIDProbeCallbackURI
			cfg.SourceIDForceReauthPromptLogin = scenario != "default_off"
			if scenario == "other_provider" {
				cfg.SourceIDEnable = false
			}
			if scenario == "duplicate_endpoint_prompt" {
				cfg.AuthorizationEndpoint += "?prompt=none&prompt=consent&vendor=kept"
			}
			if scenario == "unsupported_max_age" {
				cfg.AuthorizationEndpoint += "?max_age=0"
			}
			auth, err := s.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
			if scenario == "unsupported_max_age" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			u, err := url.Parse(auth.AuthorizationURL)
			require.NoError(t, err)
			q := u.Query()
			require.False(t, q.Has("max_age"))
			require.NotEmpty(t, q.Get("state"))
			require.NotEmpty(t, q.Get("nonce"))
			if scenario == "other_provider" || scenario == "default_off" {
				require.False(t, q.Has("prompt"))
				return
			}
			require.Equal(t, []string{"login"}, q["prompt"])
			var transaction types.OIDCTransaction
			require.NoError(t, db.First(&transaction).Error)
			var saved sourceIDReauthRequest
			require.NoError(t, json.Unmarshal([]byte(transaction.Result), &saved))
			require.Equal(t, "login", saved.Prompt)
			ctx, err := s.PrepareOIDCCallback(context.Background(), auth.State)
			require.NoError(t, err)
			require.Equal(t, "login", ctx.Value(sourceIDReauthRequestKey{}).(sourceIDReauthRequest).Prompt)
			_, err = s.PrepareOIDCCallback(context.Background(), auth.State)
			require.Error(t, err)
		})
	}
}

func TestSourceIDForcedPromptCannotBeEnabledForOldTransaction(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	s, _, _ := sourceIDLocalFixture(t)
	cfg := s.config.OIDCAuth
	cfg.AuthorizationEndpoint = "https://127.0.0.1:18443/authorize"
	cfg.TokenEndpoint = "https://127.0.0.1:18443/token"
	cfg.JwksURI = "https://127.0.0.1:18443/jwks"
	cfg.RedirectURI = approvedSourceIDProbeCallbackURI
	auth, err := s.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
	require.NoError(t, err)
	cfg.SourceIDForceReauthPromptLogin = true
	_, err = s.PrepareOIDCCallback(context.Background(), auth.State)
	require.ErrorContains(t, err, "forced reauthentication transaction required")
}

func TestSourceIDSignedAuthTimeEvidenceNeverUsesUserInfoOrInventedTime(t *testing.T) {
	cfg := &config.OIDCAuthConfig{SourceIDEnable: true, SourceIDForceReauthPromptLogin: true}
	started := time.Now().Add(-time.Second)
	ctx := context.WithValue(
		context.Background(),
		sourceIDReauthRequestKey{},
		sourceIDReauthRequest{Version: 1, Prompt: "login", Started: started},
	)
	for _, value := range []interface{}{
		float64(
			started.Add(
				-time.Minute,
			).
				Unix(),
		),
		float64(
			time.Now().
				Add(
					time.Hour,
				).
				Unix(),
		),
		"not-numeric",
		nil,
		1.25,
	} {
		require.Error(
			t,
			checkSourceIDReauthentication(ctx, cfg, map[string]interface{}{"auth_time": value}),
		)
	}
	require.NoError(
		t,
		checkSourceIDReauthentication(
			ctx,
			cfg,
			map[string]interface{}{"auth_time": float64(time.Now().Unix())},
		),
	)
	// Missing auth_time is explicitly NOT cryptographic freshness evidence.
	// Business admission still needs the independent approved provider contract.
	require.NoError(t, checkSourceIDReauthentication(ctx, cfg, map[string]interface{}{}))
	require.Error(t, checkSourceIDReauthentication(context.Background(), cfg, map[string]interface{}{}))
	decision, err := (sourceIDAuthorizationAdmissionResolver{cfg}).Resolve(
		ctx,
		types.SourceIDAdmissionRequest{
			Stage:                 "login",
			AuthorizationVerified: true,
			PromptLoginRequested:  true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, types.SourceIDAdmissionUnknown, decision.Outcome)
}

func TestSourceIDForcedReauthenticationMockSSOExpiryAndDisabledEmployee(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	for _, admin := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "ordinary", true: "system_admin_owner_cross_tenant"}[admin],
			func(t *testing.T) {
				s, db, info := sourceIDLocalFixture(t)
				s.tenantService = sourceIDLocalTestTenants{}
				cfg := s.config.OIDCAuth
				cfg.SourceIDAdmissionMode = "authorization"
				cfg.SourceIDAuthorizationRecheckConfirmed = true
				cfg.SourceIDForceReauthPromptLogin = true
				cfg.SourceIDLocalRefreshEnable = false
				s.employeeAdmission = nil
				var original types.User
				require.NoError(t, db.First(&original).Error)
				if admin {
					require.NoError(
						t,
						db.Model(&original).
							Updates(map[string]interface{}{"is_system_admin": true, "can_access_all_tenants": true}).
							Error,
					)
					require.NoError(t, db.Exec("UPDATE tenant_members SET role='owner'").Error)
				}
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				keys := jwksServer(t, key, "prompt-key")
				defer keys.Close()
				nonce := ""
				enabled := true
				mode := "supported"
				authentications := 0
				exchanges := 0
				hadCookie := false
				codeAvailable := false
				mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/authorize":
						require.Equal(t, []string{"login"}, r.URL.Query()["prompt"])
						require.False(t, r.URL.Query().Has("max_age"))
						_, cookieErr := r.Cookie("synthetic_sso")
						hadCookie = hadCookie || cookieErr == nil
						if mode == "unsupported" || !enabled {
							w.WriteHeader(http.StatusForbidden)
							return
						}
						if mode != "ignored" {
							authentications++
						}
						nonce = r.URL.Query().Get("nonce")
						codeAvailable = true
						http.SetCookie(
							w,
							&http.Cookie{
								Name:     "synthetic_sso",
								Value:    "synthetic-existing-session",
								Path:     "/",
								HttpOnly: true,
							},
						)
						w.WriteHeader(200)
					case "/token":
						exchanges++
						require.True(t, codeAvailable)
						codeAvailable = false
						require.NoError(t, r.ParseForm())
						require.Empty(t, r.PostForm.Get("code_verifier"))
						require.Empty(t, r.PostForm.Get("client_secret"))
						require.Empty(t, r.PostForm.Get("state"))
						claims := baseClaims(cfg.IssuerURL, "knowhub")
						claims["nonce"] = nonce
						if mode == "ignored" {
							claims["auth_time"] = time.Now().Add(-time.Hour).Unix()
						} else if mode != "missing_auth_time" {
							claims["auth_time"] = time.Now().Unix()
						}
						_ = json.NewEncoder(w).
							Encode(oidcTokenResponse{
								AccessToken: "synthetic-access",
								IDToken: signedIDToken(
									t,
									key,
									"prompt-key",
									claims,
								),
							})
					case "/userinfo":
						_ = json.NewEncoder(w).
							Encode(map[string]interface{}{
								"sub":        "user-123",
								"attributes": info.Claims["attributes"],
								"auth_time": time.Now().
									Unix(),
							})
					}
				}))
				defer mock.Close()
				cfg.ClientID = "knowhub"
				cfg.ClientSecret = "synthetic-secret"
				cfg.ClientAuthMethod = "client_secret_basic"
				cfg.Scopes = []string{"openid"}
				cfg.RedirectURI = approvedSourceIDProbeCallbackURI
				cfg.AuthorizationEndpoint = mock.URL + "/authorize"
				cfg.TokenEndpoint = mock.URL + "/token"
				cfg.UserInfoEndpoint = mock.URL + "/userinfo"
				cfg.JwksURI = keys.URL
				jar, err := cookiejar.New(nil)
				require.NoError(t, err)
				client := &http.Client{Jar: jar}
				login := func() (*types.OIDCCallbackResponse, error) {
					auth, err := s.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
					require.NoError(t, err)
					r, err := client.Get(auth.AuthorizationURL)
					require.NoError(t, err)
					_ = r.Body.Close()
					if r.StatusCode != 200 {
						return nil, nil
					}
					ctx, err := s.PrepareOIDCCallback(context.Background(), auth.State)
					require.NoError(t, err)
					return s.LoginWithOIDC(
						ctx,
						"synthetic-one-use-code",
						cfg.RedirectURI,
						types.TenantProvisioningTenantless,
					)
				}
				first, err := login()
				require.NoError(t, err)
				require.NotNil(t, first)
				require.Equal(t, original.ID, first.User.ID)
				require.Equal(t, admin, first.User.IsSystemAdmin)
				require.Equal(t, admin, first.User.CanAccessAllTenants)
				var session types.SourceIDSession
				require.NoError(t, db.First(&session).Error)
				require.NoError(
					t,
					db.Model(&session).Update("expires_at", time.Now().Add(-time.Second)).Error,
				)
				_, _, err = s.ValidateToken(context.Background(), first.Token)
				require.Error(t, err)
				_, _, err = s.RefreshToken(context.Background(), first.RefreshToken)
				require.Error(t, err)
				second, err := login()
				require.NoError(t, err)
				require.Equal(t, original.ID, second.User.ID)
				require.True(t, hadCookie)
				require.Equal(t, 2, authentications)
				require.Equal(t, 2, exchanges)
				mode = "ignored"
				_, err = login()
				require.ErrorContains(t, err, "auth_time invalid or stale")
				require.Equal(t, 3, exchanges)
				mode = "missing_auth_time"
				cfg.SourceIDAuthorizationRecheckConfirmed = false
				_, err = login()
				require.ErrorContains(t, err, "contract pending")
				require.Equal(t, 4, exchanges)
				cfg.SourceIDAuthorizationRecheckConfirmed = true
				mode = "unsupported"
				denied, err := login()
				require.NoError(t, err)
				require.Nil(t, denied)
				require.Equal(t, 4, exchanges)
				mode = "supported"
				enabled = false
				denied, err = login()
				require.NoError(t, err)
				require.Nil(t, denied)
				require.Equal(t, 4, exchanges)
				var sessions int64
				require.NoError(t, db.Model(&types.SourceIDSession{}).Count(&sessions).Error)
				require.EqualValues(t, 2, sessions)
				var users int64
				require.NoError(t, db.Model(&types.User{}).Count(&users).Error)
				require.EqualValues(t, 1, users)
				var role string
				require.NoError(t, db.Table("tenant_members").Select("role").Row().Scan(&role))
				require.Equal(t, map[bool]string{false: "contributor", true: "owner"}[admin], role)
			},
		)
	}
}
