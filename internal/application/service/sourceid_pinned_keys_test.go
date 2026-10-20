package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func pinnedTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, e)
	return k
}

func pinnedTestJWK(k *rsa.PrivateKey) sidPinnedJWK {
	j := rsaJWK(k, "")
	return sidPinnedJWK{Kty: j.Kty, N: j.N, E: j.E, Alg: j.Alg, Use: j.Use}
}

func pinnedTestConfig(
	t *testing.T,
	keys ...*rsa.PrivateKey,
) (*config.OIDCAuthConfig, *sidPinnedKeyDocument) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	c := &config.OIDCAuthConfig{
		Enable:                        true,
		SourceIDEnable:                true,
		SourceIDContractConfirmed:     true,
		SourceIDValidationStrategy:    config.SourceIDPinnedValidation,
		IssuerURL:                     config.SourceIDPinnedIssuer,
		ClientID:                      "synthetic-knowhub",
		ClientSecret:                  "synthetic-only",
		SourceIDPinnedKeysConfirmed:   true,
		SourceIDPinnedKeysApprovalRef: "SYNTHETIC-NOT-A-REAL-APPROVAL",
		SourceIDPinnedKeysExpiresAt:   now.Add(time.Hour).Format(time.RFC3339),
		SourceIDPinnedKeysFile:        filepath.Join(t.TempDir(), "keys.json"),
		UserInfoMapping:               &config.OIDCUserInfoMapping{Username: "name", Email: "email"},
	}
	d := &sidPinnedKeyDocument{
		Version:     1,
		Issuer:      c.IssuerURL,
		ClientID:    c.ClientID,
		ApprovalRef: c.SourceIDPinnedKeysApprovalRef,
		NotBefore:   now.Add(-time.Minute),
		ExpiresAt:   now.Add(30 * time.Minute),
	}
	fps := []string{}
	for _, k := range keys {
		d.Keys = append(d.Keys, pinnedTestJWK(k))
		fps = append(fps, sidPinnedFingerprint(&k.PublicKey))
	}
	c.SourceIDPinnedKeyFingerprints = strings.Join(fps, ",")
	pinnedWrite(t, c, d)
	return c, d
}

func pinnedWrite(t *testing.T, c *config.OIDCAuthConfig, d *sidPinnedKeyDocument) {
	t.Helper()
	raw, e := json.Marshal(d)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(c.SourceIDPinnedKeysFile, raw, 0o600))
}

func pinnedToken(
	t *testing.T,
	k *rsa.PrivateKey,
	c *config.OIDCAuthConfig,
	change func(jwt.MapClaims),
	header map[string]interface{},
) string {
	t.Helper()
	claims := baseClaims(c.IssuerURL, c.ClientID)
	claims["nonce"] = "expected-nonce"
	if change != nil {
		change(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	for k, v := range header {
		token.Header[k] = v
	}
	raw, e := token.SignedString(k)
	require.NoError(t, e)
	return raw
}

func TestSIDPinnedKeysSignaturesAndClaims(t *testing.T) {
	a, b, c := pinnedTestKey(t), pinnedTestKey(t), pinnedTestKey(t)
	cfg, _ := pinnedTestConfig(t, a, b)
	svc := &userService{}
	for _, tc := range []struct {
		name   string
		key    *rsa.PrivateKey
		change func(jwt.MapClaims)
		header map[string]interface{}
		want   bool
	}{
		{name: "A_no_kid", key: a, want: true},
		{name: "B_no_kid", key: b, want: true},
		{name: "untrusted_C", key: c},
		{name: "issuer", key: a, change: func(v jwt.MapClaims) { v["iss"] = "https://attacker.invalid" }},
		{name: "audience", key: a, change: func(v jwt.MapClaims) { v["aud"] = "wrong-client" }},
		{
			name: "expired",
			key:  a,
			change: func(v jwt.MapClaims) {
				v["exp"] = time.Now().
					Add(
						-10 * time.Minute,
					).
					Unix()
			},
		},
		{
			name: "future_nbf",
			key:  a,
			change: func(v jwt.MapClaims) {
				v["nbf"] = time.Now().
					Add(
						10 * time.Minute,
					).
					Unix()
			},
		},
		{
			name: "future_iat",
			key:  a,
			change: func(v jwt.MapClaims) {
				v["iat"] = time.Now().
					Add(
						10 * time.Minute,
					).
					Unix()
			},
		},
		{name: "missing_iat", key: a, change: func(v jwt.MapClaims) { delete(v, "iat") }},
		{name: "missing_exp", key: a, change: func(v jwt.MapClaims) { delete(v, "exp") }},
		{name: "empty_sub", key: a, change: func(v jwt.MapClaims) { v["sub"] = "" }},
		{name: "numeric_sub", key: a, change: func(v jwt.MapClaims) { v["sub"] = 123 }},
		{name: "wrong_azp", key: a, change: func(v jwt.MapClaims) { v["azp"] = "another-client" }},
		{
			name: "multiple_audience_no_azp",
			key:  a,
			change: func(v jwt.MapClaims) {
				v["aud"] = []string{
					cfg.ClientID,
					"other",
				}
			},
		},
		{
			name: "multiple_audience_correct_azp",
			key:  a,
			change: func(v jwt.MapClaims) {
				v["aud"] = []string{
					cfg.ClientID,
					"other",
				}
				v["azp"] = cfg.ClientID
			},
			want: true,
		},
		{name: "unknown_kid", key: a, header: map[string]interface{}{"kid": "unknown"}},
		{name: "nonstring_kid", key: a, header: map[string]interface{}{"kid": 1}},
		{name: "jku", key: a, header: map[string]interface{}{"jku": "https://attacker.invalid/keys"}},
		{name: "jwk", key: a, header: map[string]interface{}{"jwk": map[string]interface{}{"kty": "RSA"}}},
		{name: "x5u", key: a, header: map[string]interface{}{"x5u": "https://attacker.invalid/cert"}},
		{name: "critical", key: a, header: map[string]interface{}{"crit": []string{"custom"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := svc.verifyOIDCIDToken(
				context.Background(),
				cfg,
				pinnedToken(t, tc.key, cfg, tc.change, tc.header),
			)
			if tc.want {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		_, e := svc.verifyOIDCIDToken(
			context.Background(),
			cfg,
			unsignedJWT(baseClaims(cfg.IssuerURL, cfg.ClientID)),
		)
		require.Error(t, e)
	})
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodRS384, jwt.SigningMethodPS256, jwt.SigningMethodHS256} {
		t.Run(method.Alg(), func(t *testing.T) {
			var key interface{} = a
			if method == jwt.SigningMethodHS256 {
				key = []byte("synthetic-hmac")
			}
			raw, e := jwt.NewWithClaims(method, baseClaims(cfg.IssuerURL, cfg.ClientID)).SignedString(key)
			require.NoError(t, e)
			_, e = svc.verifyOIDCIDToken(context.Background(), cfg, raw)
			require.Error(t, e)
		})
	}
}

func TestSIDPinnedKeysControlledLifecycle(t *testing.T) {
	a, b, c := pinnedTestKey(t), pinnedTestKey(t), pinnedTestKey(t)
	for _, name := range []string{
		"duplicate_material",
		"unknown_key",
		"revoked",
		"future_set",
		"expired_set",
		"extended_deadline",
		"wrong_approval",
		"wrong_issuer",
		"wrong_client",
		"wrong_version",
		"key_limit",
		"weak_key",
		"wrong_use",
		"wrong_alg",
		"wrong_ops",
		"bad_n",
		"bad_e",
		"non_rsa",
		"oversize_file",
		"private_jwk_field",
		"missing_file",
		"unsafe_permissions",
		"malformed",
		"duplicate_json",
		"symlink",
		"missing_confirmation",
		"expired_config",
		"unknown_strategy",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, d := pinnedTestConfig(t, a, b)
			want := false
			switch name {
			case "duplicate_material":
				d.Keys = append(d.Keys, d.Keys[0])
				want = true
			case "unknown_key":
				d.Keys = append(d.Keys, pinnedTestJWK(c))
			case "revoked":
				d.Revoked = true
			case "future_set":
				d.NotBefore = time.Now().Add(time.Minute)
			case "expired_set":
				d.ExpiresAt = time.Now().Add(-time.Minute)
			case "extended_deadline":
				d.ExpiresAt = time.Now().Add(2 * time.Hour)
			case "wrong_approval":
				d.ApprovalRef = "unapproved"
			case "wrong_issuer":
				d.Issuer = "https://wrong.invalid"
			case "wrong_client":
				d.ClientID = "wrong"
			case "wrong_version":
				d.Version = 2
			case "key_limit":
				for len(d.Keys) <= sidPinnedMaxKeys {
					d.Keys = append(d.Keys, d.Keys[0])
				}
			case "weak_key":
				k, e := rsa.GenerateKey(rand.Reader, 1024)
				require.NoError(t, e)
				d.Keys[0] = pinnedTestJWK(k)
			case "wrong_use":
				d.Keys[0].Use = "enc"
			case "wrong_alg":
				d.Keys[0].Alg = "RS384"
			case "wrong_ops":
				d.Keys[0].KeyOps = []string{"sign"}
			case "bad_n":
				d.Keys[0].N = "not-a-valid-modulus!"
			case "bad_e":
				d.Keys[0].E = "Aw"
			case "non_rsa":
				d.Keys[0].Kty = "EC"
			case "missing_confirmation":
				cfg.SourceIDPinnedKeysConfirmed = false
			case "expired_config":
				cfg.SourceIDPinnedKeysExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
			case "unknown_strategy":
				cfg.SourceIDValidationStrategy = "unsafe"
			}
			pinnedWrite(t, cfg, d)
			switch name {
			case "private_jwk_field":
				raw, e := os.ReadFile(cfg.SourceIDPinnedKeysFile)
				require.NoError(t, e)
				raw = bytesReplaceFirst(raw, `"kty":"RSA"`, `"d":"not-allowed","kty":"RSA"`)
				require.NoError(t, os.WriteFile(cfg.SourceIDPinnedKeysFile, raw, 0o600))
			case "missing_file":
				require.NoError(t, os.Remove(cfg.SourceIDPinnedKeysFile))
			case "unsafe_permissions":
				require.NoError(t, os.Chmod(cfg.SourceIDPinnedKeysFile, 0o666))
			case "malformed":
				require.NoError(t, os.WriteFile(cfg.SourceIDPinnedKeysFile, []byte("{"), 0o600))
			case "oversize_file":
				require.NoError(
					t,
					os.WriteFile(
						cfg.SourceIDPinnedKeysFile,
						[]byte(strings.Repeat(" ", sidPinnedMaxBytes+1)),
						0o600,
					),
				)
			case "duplicate_json":
				raw, e := os.ReadFile(cfg.SourceIDPinnedKeysFile)
				require.NoError(t, e)
				raw = append([]byte(`{"version":1,`), raw[1:]...)
				require.NoError(t, os.WriteFile(cfg.SourceIDPinnedKeysFile, raw, 0o600))
			case "symlink":
				old := cfg.SourceIDPinnedKeysFile
				cfg.SourceIDPinnedKeysFile += ".link"
				require.NoError(t, os.Symlink(old, cfg.SourceIDPinnedKeysFile))
			}
			set, e := loadSIDPinnedKeys(cfg, time.Now())
			if want {
				require.NoError(t, e)
				require.Len(t, set.candidates, 2)
			} else {
				require.Error(t, e)
			}
		})
	}
	cfg, d := pinnedTestConfig(t, a, b)
	svc := &userService{}
	token := pinnedToken(t, a, cfg, nil, nil)
	_, e := svc.verifyOIDCIDToken(context.Background(), cfg, token)
	require.NoError(t, e)
	d.Keys = []sidPinnedJWK{pinnedTestJWK(b)}
	pinnedWrite(t, cfg, d)
	_, e = svc.verifyOIDCIDToken(context.Background(), cfg, token)
	require.Error(t, e, "removed key must not survive a successful-verify cache")
	_, e = svc.verifyOIDCIDToken(context.Background(), cfg, pinnedToken(t, b, cfg, nil, nil))
	require.NoError(t, e)
}

func bytesReplaceFirst(raw []byte, old, replacement string) []byte {
	return []byte(strings.Replace(string(raw), old, replacement, 1))
}

func TestSIDPinnedKeysAmbiguousMatchDefense(t *testing.T) {
	a := pinnedTestKey(t)
	digest := sha256.Sum256([]byte("synthetic-cardinality-test"))
	sig, e := rsa.SignPKCS1v15(rand.Reader, a, crypto.SHA256, digest[:])
	require.NoError(t, e)
	// Deliberately violate loader deduplication to exercise the last cardinality
	// defense. This is not a claim that two natural RSA materials collide.
	_, e = uniqueSIDPinnedSignatureMatch(
		[]sidPinnedCandidate{{key: &a.PublicKey}, {key: &a.PublicKey}},
		"",
		digest[:],
		sig,
	)
	require.Error(t, e)
}

func TestSIDPinnedKeysAmbiguousJWTSyntax(t *testing.T) {
	a := pinnedTestKey(t)
	cfg, _ := pinnedTestConfig(t, a)
	claims, e := json.Marshal(baseClaims(cfg.IssuerURL, cfg.ClientID))
	require.NoError(t, e)
	for _, tc := range []struct{ name, header, payload string }{
		{"duplicate_alg", `{"alg":"RS256","alg":"RS256"}`, string(claims)},
		{
			"duplicate_sub",
			`{"alg":"RS256"}`,
			strings.Replace(
				string(
					claims,
				),
				`"sub":"user-123"`,
				`"sub":"other","sub":"user-123"`,
				1,
			),
		},
		{"trailing_claims", `{"alg":"RS256"}`, string(claims) + ` {}`},
		{"null_kid", `{"alg":"RS256","kid":null}`, string(claims)},
		{"oversize", `{"alg":"RS256","unused":"` + strings.Repeat("a", 16<<10) + `"}`, string(claims)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := base64.RawURLEncoding.EncodeToString([]byte(tc.header))
			p := base64.RawURLEncoding.EncodeToString([]byte(tc.payload))
			digest := sha256.Sum256([]byte(h + "." + p))
			sig, e := rsa.SignPKCS1v15(rand.Reader, a, crypto.SHA256, digest[:])
			require.NoError(t, e)
			raw := h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
			_, e = (&userService{}).verifyOIDCIDToken(context.Background(), cfg, raw)
			require.Error(t, e)
		})
	}
}

func TestSIDPinnedKeyIDsAndCanonicalDedup(t *testing.T) {
	a, b := pinnedTestKey(t), pinnedTestKey(t)
	cfg, d := pinnedTestConfig(t, a, b)
	d.Keys[0].Kid = "A"
	d.Keys[1].Kid = "B"
	pinnedWrite(t, cfg, d)
	_, e := (&userService{}).verifyOIDCIDToken(
		context.Background(),
		cfg,
		pinnedToken(t, a, cfg, nil, map[string]interface{}{"kid": "A"}),
	)
	require.NoError(t, e)
	_, e = (&userService{}).verifyOIDCIDToken(
		context.Background(),
		cfg,
		pinnedToken(t, a, cfg, nil, map[string]interface{}{"kid": "B"}),
	)
	require.Error(t, e)
	d.Keys[1].Kid = "A"
	pinnedWrite(t, cfg, d)
	_, e = loadSIDPinnedKeys(cfg, time.Now())
	require.Error(t, e)
	d.Keys[1].Kid = "B"
	duplicate := d.Keys[0]
	n, e := base64.RawURLEncoding.DecodeString(duplicate.N)
	require.NoError(t, e)
	duplicate.N = base64.RawURLEncoding.EncodeToString(append([]byte{0}, n...))
	duplicate.Kid = "alias-A"
	d.Keys = append(d.Keys, duplicate)
	pinnedWrite(t, cfg, d)
	set, e := loadSIDPinnedKeys(cfg, time.Now())
	require.NoError(t, e)
	require.Len(t, set.candidates, 2)
}

type sidPinnedRouteTransport struct {
	target string
	inner  http.RoundTripper
}

func (r sidPinnedRouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	u := *req.URL
	target, err := http.NewRequest(http.MethodGet, r.target, nil)
	if err != nil {
		return nil, err
	}
	u.Scheme = target.URL.Scheme
	u.Host = target.URL.Host
	cloned.URL = &u
	cloned.Host = u.Host
	return r.inner.RoundTrip(cloned)
}

func TestSIDPinnedKeysNonceAndUserInfo(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.ruijie.com.cn")
	a := pinnedTestKey(t)
	for _, name := range []string{
		"valid",
		"wrong_nonce",
		"missing_nonce",
		"userinfo_error",
		"userinfo_subject",
		"missing_access",
		"revoked_during_userinfo",
		"userinfo_redirect",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, d := pinnedTestConfig(t, a)
			cfg.UserInfoEndpoint = config.SourceIDPinnedIssuer + "/oidc/profile"
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch name {
				case "userinfo_error":
					w.WriteHeader(503)
					return
				case "userinfo_redirect":
					http.Redirect(w, r, "https://attacker.invalid/", http.StatusFound)
					return
				case "revoked_during_userinfo":
					d.Revoked = true
					pinnedWrite(t, cfg, d)
				}
				sub := "user-123"
				if name == "userinfo_subject" {
					sub = "other-user"
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": sub, "name": "synthetic"})
			}))
			defer server.Close()
			client := server.Client()
			client.Transport = sidPinnedRouteTransport{target: server.URL, inner: client.Transport}
			svc := &userService{protocolClient: client}
			ctx := context.WithValue(context.Background(), oidcNonceKey, "expected-nonce")
			if name == "missing_nonce" {
				ctx = context.Background()
			}
			raw := pinnedToken(t, a, cfg, func(c jwt.MapClaims) {
				if name == "wrong_nonce" {
					c["nonce"] = "wrong"
				}
			}, nil)
			access := "synthetic-access"
			if name == "missing_access" {
				access = ""
			}
			info, e := svc.resolveOIDCUserInfo(
				ctx,
				cfg,
				&oidcTokenResponse{IDToken: raw, AccessToken: access},
			)
			if name == "valid" {
				require.NoError(t, e)
				require.Equal(t, "user-123", info.Subject)
			} else {
				require.Error(t, e)
				require.Nil(t, info)
			}
			if name == "wrong_nonce" || name == "missing_nonce" || name == "missing_access" {
				require.Zero(t, calls)
			}
		})
	}
}

func TestSIDPinnedKeysLoginFailuresHaveNoIdentitySideEffects(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.ruijie.com.cn")
	a, c := pinnedTestKey(t), pinnedTestKey(t)
	for _, failure := range []string{
		"untrusted_key",
		"nonce",
		"token_error",
		"userinfo_error",
		"userinfo_sub",
		"invalid_tls",
		"missing_access",
		"state_replay",
		"state_invalid",
		"repeated_code",
	} {
		t.Run(failure, func(t *testing.T) {
			cfg, _ := pinnedTestConfig(t, a)
			cfg.AuthorizationEndpoint = config.SourceIDPinnedIssuer + "/oidc/authorize"
			cfg.TokenEndpoint = config.SourceIDPinnedIssuer + "/oidc/accessToken"
			cfg.UserInfoEndpoint = config.SourceIDPinnedIssuer + "/oidc/profile"
			cfg.JwksURI = config.SourceIDPinnedIssuer + "/oidc/jwks"
			cfg.RedirectURI = approvedSourceIDProbeCallbackURI
			cfg.ClientAuthMethod = "client_secret_basic"
			cfg.Scopes = []string{"openid"}
			db, e := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "identity.db")), &gorm.Config{})
			require.NoError(t, e)
			connection, e := db.DB()
			require.NoError(t, e)
			defer func(cleanup func() error) { _ = cleanup() }(connection.Close)
			require.NoError(
				t,
				db.AutoMigrate(
					&types.User{},
					&types.AuthToken{},
					&types.ExternalIdentity{},
					&types.SourceIDSession{},
					&types.OIDCTransaction{},
				),
			)
			nonce := ""
			calls := 0
			tokenCalls := 0
			profileCalls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/oidc/profile" {
					profileCalls++
					if failure == "userinfo_error" || failure == "repeated_code" {
						w.WriteHeader(503)
						return
					}
					sub := "user-123"
					if failure == "userinfo_sub" {
						sub = "other"
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": sub})
					return
				}
				tokenCalls++
				if failure == "token_error" || (failure == "repeated_code" && tokenCalls > 1) {
					w.WriteHeader(401)
					return
				}
				key := a
				if failure == "untrusted_key" {
					key = c
				}
				token := pinnedToken(t, key, cfg, func(v jwt.MapClaims) {
					v["nonce"] = nonce
					if failure == "nonce" {
						v["nonce"] = "wrong"
					}
				}, nil)
				access := "synthetic-access"
				if failure == "missing_access" {
					access = ""
				}
				_ = json.NewEncoder(w).
					Encode(oidcTokenResponse{IDToken: token, AccessToken: access, TokenType: "Bearer"})
			}))
			defer server.Close()
			client := server.Client()
			if failure == "invalid_tls" {
				client = &http.Client{Transport: http.DefaultTransport}
			}
			client.Transport = sidPinnedRouteTransport{target: server.URL, inner: client.Transport}
			svc := &userService{
				config:         &config.Config{OIDCAuth: cfg},
				userRepo:       apprepo.NewUserRepository(db),
				tokenRepo:      apprepo.NewAuthTokenRepository(db),
				protocolClient: client,
			}
			auth, e := svc.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
			require.NoError(t, e)
			nonce = auth.Nonce
			if failure == "state_invalid" {
				_, e = svc.PrepareOIDCCallback(context.Background(), auth.State+"tamper")
				require.Error(t, e)
				require.Zero(t, calls)
				for _, table := range []string{"users", "auth_tokens", "external_identities", "source_id_sessions"} {
					var n int64
					require.NoError(t, db.Table(table).Count(&n).Error)
					require.Zero(t, n, table)
				}
				return
			}
			ctx, e := svc.PrepareOIDCCallback(context.Background(), auth.State)
			require.NoError(t, e)
			if failure == "state_replay" {
				_, e = svc.PrepareOIDCCallback(context.Background(), auth.State)
				require.Error(t, e)
				require.Zero(t, calls)
			} else {
				response, e := svc.LoginWithOIDC(
					ctx,
					"synthetic-once-code",
					cfg.RedirectURI,
					types.TenantProvisioningTenantless,
				)
				require.Error(t, e)
				require.Nil(t, response)
			}
			if failure == "repeated_code" {
				auth, e = svc.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
				require.NoError(t, e)
				nonce = auth.Nonce
				ctx, e = svc.PrepareOIDCCallback(context.Background(), auth.State)
				require.NoError(t, e)
				response, e := svc.LoginWithOIDC(
					ctx,
					"synthetic-once-code",
					cfg.RedirectURI,
					types.TenantProvisioningTenantless,
				)
				require.Error(t, e)
				require.Nil(t, response)
				require.Equal(t, 2, tokenCalls)
				require.Equal(t, 1, profileCalls)
			}
			for _, table := range []string{"users", "auth_tokens", "external_identities", "source_id_sessions"} {
				var n int64
				require.NoError(t, db.Table(table).Count(&n).Error)
				require.Zero(t, n, table)
			}
		})
	}
}

func TestSIDPinnedKeysParallelVerification(t *testing.T) {
	a, b := pinnedTestKey(t), pinnedTestKey(t)
	cfg, _ := pinnedTestConfig(t, a, b)
	tokens := []string{pinnedToken(t, a, cfg, nil, nil), pinnedToken(t, b, cfg, nil, nil)}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := (&userService{}).verifyOIDCIDToken(context.Background(), cfg, tokens[i%2])
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
}

func TestSIDPinnedRollbackToJWKS(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	a, b := pinnedTestKey(t), pinnedTestKey(t)
	cfg, _ := pinnedTestConfig(t, a, b)
	online := jwksServer(t, b, "")
	defer online.Close()
	svc := &userService{}
	tokenA, tokenB := pinnedToken(t, a, cfg, nil, nil), pinnedToken(t, b, cfg, nil, nil)
	_, e := svc.verifyOIDCIDToken(context.Background(), cfg, tokenA)
	require.NoError(t, e)
	cfg.SourceIDValidationStrategy = config.SourceIDJWKSValidation
	cfg.JwksURI = online.URL
	require.NoError(t, os.Remove(cfg.SourceIDPinnedKeysFile))
	_, e = svc.verifyOIDCIDToken(context.Background(), cfg, tokenB)
	require.NoError(t, e)
	_, e = svc.verifyOIDCIDToken(context.Background(), cfg, tokenA)
	require.Error(t, e, "JWKS failure cannot fall back to the previously approved file")
}

type sidPinnedRejectNetwork struct{ calls int }

func (t *sidPinnedRejectNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("network must not be used by fixed-key verification")
}

func TestSIDPinnedKeysNeverLearnFromNetwork(t *testing.T) {
	a, c := pinnedTestKey(t), pinnedTestKey(t)
	cfg, _ := pinnedTestConfig(t, a)
	cfg.JwksURI = config.SourceIDPinnedIssuer + "/oidc/jwks"
	network := &sidPinnedRejectNetwork{}
	svc := &userService{protocolClient: &http.Client{Transport: network}}
	_, e := svc.verifyOIDCIDToken(context.Background(), cfg, pinnedToken(t, a, cfg, nil, nil))
	require.NoError(t, e)
	_, e = svc.verifyOIDCIDToken(context.Background(), cfg, pinnedToken(t, c, cfg, nil, nil))
	require.Error(t, e)
	require.Zero(t, network.calls)
}

func TestSIDPinnedKeysSyntheticExistingAccountLifecycle(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.ruijie.com.cn")
	svc, db, info, _, originalID := sourceIDSSOFixture(t)
	a, b := pinnedTestKey(t), pinnedTestKey(t)
	pin, _ := pinnedTestConfig(t, a, b)
	c := svc.config.OIDCAuth
	c.SourceIDValidationStrategy = pin.SourceIDValidationStrategy
	c.SourceIDPinnedKeysFile = pin.SourceIDPinnedKeysFile
	c.SourceIDPinnedKeysConfirmed = pin.SourceIDPinnedKeysConfirmed
	c.SourceIDPinnedKeysApprovalRef = pin.SourceIDPinnedKeysApprovalRef
	c.SourceIDPinnedKeysExpiresAt = pin.SourceIDPinnedKeysExpiresAt
	c.SourceIDPinnedKeyFingerprints = pin.SourceIDPinnedKeyFingerprints
	c.IssuerURL = pin.IssuerURL
	c.ClientID = pin.ClientID
	c.ClientSecret = pin.ClientSecret
	c.RedirectURI = approvedSourceIDProbeCallbackURI
	c.ClientAuthMethod = "client_secret_basic"
	c.Scopes = []string{"openid"}
	c.AuthorizationEndpoint = c.IssuerURL + "/oidc/authorize"
	c.TokenEndpoint = c.IssuerURL + "/oidc/accessToken"
	c.UserInfoEndpoint = c.IssuerURL + "/oidc/profile"
	c.JwksURI = c.IssuerURL + "/oidc/jwks"
	raw, e := os.ReadFile(c.SourceIDPilotAccountsFile)
	require.NoError(t, e)
	var ownership map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &ownership))
	ownership["issuer"] = c.IssuerURL
	raw, e = json.Marshal(ownership)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(c.SourceIDPilotAccountsFile, raw, 0o600))
	svc.tenantService = sourceIDLocalTestTenants{}
	svc.memberService = sourceIDLocalTestMembers{userID: originalID}
	nonce := ""
	signer := a
	jwksCalls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oidc/profile":
			profile := map[string]interface{}{}
			for k, v := range info.Claims {
				profile[k] = v
			}
			profile["sub"] = info.Subject
			_ = json.NewEncoder(w).Encode(profile)
		case "/oidc/accessToken":
			token := pinnedToken(
				t,
				signer,
				c,
				func(v jwt.MapClaims) { v["nonce"] = nonce; v["sub"] = info.Subject },
				nil,
			)
			_ = json.NewEncoder(w).
				Encode(oidcTokenResponse{IDToken: token, AccessToken: "synthetic-access", TokenType: "Bearer"})
		default:
			jwksCalls++
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	client := server.Client()
	client.Transport = sidPinnedRouteTransport{target: server.URL, inner: client.Transport}
	svc.protocolClient = client
	for _, key := range []*rsa.PrivateKey{a, b} {
		signer = key
		auth, e := svc.GetOIDCAuthorizationURL(context.Background(), c.RedirectURI)
		require.NoError(t, e)
		nonce = auth.Nonce
		ctx, e := svc.PrepareOIDCCallback(context.Background(), auth.State)
		require.NoError(t, e)
		response, e := svc.LoginWithOIDC(
			ctx,
			"synthetic-code",
			c.RedirectURI,
			types.TenantProvisioningTenantless,
		)
		require.NoError(t, e)
		require.True(t, response.Success)
		require.Equal(t, originalID, response.User.ID)
		require.False(t, response.User.IsSystemAdmin)
		require.False(t, response.User.CanAccessAllTenants)
		require.EqualValues(t, 42, response.User.TenantID)
		_, _, e = svc.ValidateToken(context.Background(), response.Token)
		require.NoError(t, e)
		require.NoError(t, svc.Logout(context.Background(), response.Token))
		_, _, e = svc.ValidateToken(context.Background(), response.Token)
		require.Error(t, e)
	}
	require.Zero(t, jwksCalls)
	var users, bindings int64
	require.NoError(t, db.Model(&types.User{}).Count(&users).Error)
	require.EqualValues(t, 1, users)
	require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&bindings).Error)
	require.EqualValues(t, 1, bindings)
	var role string
	require.NoError(
		t,
		db.Raw("SELECT role FROM tenant_members WHERE user_id=?", originalID).Scan(&role).Error,
	)
	require.Equal(t, "contributor", role)
}
