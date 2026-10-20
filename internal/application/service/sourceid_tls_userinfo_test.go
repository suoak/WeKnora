package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type sidTLSFixture struct {
	s                          *userService
	db                         *gorm.DB
	info                       *types.OIDCUserInfo
	uid                        string
	server                     *httptest.Server
	signer                     *rsa.PrivateKey
	mu                         sync.Mutex
	nonces                     map[string]string
	claimsEdit                 func(jwt.MapClaims)
	headerEdit                 func(map[string]interface{})
	tokenEdit                  func(map[string]interface{})
	profileEdit                func(map[string]interface{})
	tokenStatus, profileStatus int
	rawTokenJSON               string
	profileDelay               time.Duration
	tokenCalls, profileCalls   atomic.Int64
}

func newSIDTLSFixture(t *testing.T) *sidTLSFixture {
	t.Helper()
	withOIDCSSRFWhitelist(t, "sid.ruijie.com.cn")
	s, db, info, _, uid := sourceIDSSOFixture(t)
	f := &sidTLSFixture{s: s, db: db, info: info, uid: uid, signer: pinnedTestKey(t), nonces: map[string]string{}, tokenStatus: 200, profileStatus: 200}
	c := s.config.OIDCAuth
	c.Enable = true
	c.SourceIDValidationStrategy = config.SourceIDTLSUserInfoValidation
	c.SourceIDTLSUserInfoConfirmed = true
	c.SourceIDTLSUserInfoApprovalRef = "SYNTHETIC-ONLY"
	c.SourceIDTLSUserInfoExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	c.IssuerURL = config.SourceIDPinnedIssuer
	c.ClientID = "synthetic-tls-client"
	c.ClientSecret = "synthetic-tls-secret"
	c.RedirectURI = config.SourceIDHTTPCallback
	c.ClientAuthMethod = "client_secret_basic"
	c.SourceIDClaimsSource = "userinfo"
	c.AuthorizationEndpoint = c.IssuerURL + "/oidc/authorize"
	c.TokenEndpoint = c.IssuerURL + "/oidc/accessToken"
	c.UserInfoEndpoint = c.IssuerURL + "/oidc/profile"
	c.JwksURI = ""
	c.Scopes = []string{"openid"}
	if c.UserInfoMapping == nil {
		c.UserInfoMapping = &config.OIDCUserInfoMapping{Username: "name", Email: "email"}
	}
	raw, e := os.ReadFile(c.SourceIDPilotAccountsFile)
	require.NoError(t, e)
	var ownership map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &ownership))
	ownership["issuer"] = c.IssuerURL
	raw, e = json.Marshal(ownership)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(c.SourceIDPilotAccountsFile, raw, 0600))
	s.tenantService = sourceIDLocalTestTenants{}
	s.memberService = sourceIDLocalTestMembers{userID: uid}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, e)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic SID test only"}, DNSNames: []string{"sid.ruijie.com.cn"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, e)
	public, e := x509.ParseCertificate(der)
	require.NoError(t, e)
	pool := x509.NewCertPool()
	pool.AddCert(public)
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/oidc/accessToken":
			f.tokenCalls.Add(1)
			_ = r.ParseForm()
			id, secret, ok := r.BasicAuth()
			if !ok || id != c.ClientID || secret != c.ClientSecret || r.Form.Get("redirect_uri") != c.RedirectURI {
				w.WriteHeader(401)
				return
			}
			if f.tokenStatus != 200 {
				w.Header().Set("Location", "https://untrusted.invalid/token")
				w.WriteHeader(f.tokenStatus)
				return
			}
			if f.rawTokenJSON != "" {
				_, _ = io.WriteString(w, f.rawTokenJSON)
				return
			}
			code := r.Form.Get("code")
			nonce, ok := f.nonces[code]
			if !ok {
				w.WriteHeader(400)
				return
			}
			claims := baseClaims(c.IssuerURL, c.ClientID)
			claims["sub"] = info.Subject
			claims["nonce"] = nonce
			claims["attributes"] = info.Claims["attributes"]
			if f.claimsEdit != nil {
				f.claimsEdit(claims)
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			if f.headerEdit != nil {
				f.headerEdit(token.Header)
			}
			signed, e := token.SignedString(f.signer)
			if e != nil {
				w.WriteHeader(500)
				return
			}
			data := map[string]interface{}{"id_token": signed, "access_token": "synthetic-access-token", "token_type": "Bearer"}
			if f.tokenEdit != nil {
				f.tokenEdit(data)
			}
			_ = json.NewEncoder(w).Encode(data)
		case "/oidc/profile":
			f.profileCalls.Add(1)
			if f.profileDelay > 0 {
				time.Sleep(f.profileDelay)
			}
			if f.profileStatus != 200 {
				w.Header().Set("Location", "https://untrusted.invalid/profile")
				w.WriteHeader(f.profileStatus)
				return
			}
			if r.Method == http.MethodGet && r.Header.Get("Authorization") != "Bearer synthetic-access-token" {
				w.WriteHeader(401)
				return
			}
			b, _ := json.Marshal(info.Claims)
			var data map[string]interface{}
			_ = json.Unmarshal(b, &data)
			data["sub"] = info.Subject
			if f.profileEdit != nil {
				f.profileEdit(data)
			}
			_ = json.NewEncoder(w).Encode(data)
		default:
			w.WriteHeader(500)
		}
	}))
	f.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "sid.ruijie.com.cn:443" {
			return nil, errors.New("unexpected destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, f.server.Listener.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	s.protocolClient = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	sqlDB, e := db.DB()
	require.NoError(t, e)
	sqlDB.SetMaxOpenConns(1)
	return f
}

func (f *sidTLSFixture) begin(t *testing.T, code string) (context.Context, string) {
	t.Helper()
	auth, e := f.s.GetOIDCAuthorizationURL(context.Background(), f.s.config.OIDCAuth.RedirectURI)
	require.NoError(t, e)
	f.mu.Lock()
	f.nonces[code] = auth.Nonce
	f.mu.Unlock()
	ctx, e := f.s.PrepareOIDCCallback(context.Background(), auth.State)
	require.NoError(t, e)
	return ctx, auth.State
}

func sidTLSBusinessCounts(t *testing.T, db *gorm.DB) []int64 {
	t.Helper()
	result := []int64{}
	for _, table := range []string{"users", "external_identities", "auth_tokens", "source_id_sessions", "source_id_local_sessions"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		result = append(result, count)
	}
	return result
}

func TestSIDTLSUserInfoExistingAccountLifecycle(t *testing.T) {
	f := newSIDTLSFixture(t)
	for _, code := range []string{"synthetic-first", "synthetic-repeat"} {
		ctx, _ := f.begin(t, code)
		response, e := f.s.LoginWithOIDC(ctx, code, f.s.config.OIDCAuth.RedirectURI, types.TenantProvisioningTenantless)
		require.NoError(t, e)
		require.True(t, response.Success)
		require.Equal(t, f.uid, response.User.ID)
		require.False(t, response.User.IsSystemAdmin)
		require.False(t, response.User.CanAccessAllTenants)
		require.Empty(t, response.RefreshToken)
		_, _, e = f.s.ValidateToken(context.Background(), response.Token)
		require.NoError(t, e)
		require.NoError(t, f.s.Logout(context.Background(), response.Token))
		_, _, e = f.s.ValidateToken(context.Background(), response.Token)
		require.Error(t, e)
	}
	var users, bindings int64
	require.NoError(t, f.db.Model(&types.User{}).Count(&users).Error)
	require.NoError(t, f.db.Model(&types.ExternalIdentity{}).Count(&bindings).Error)
	require.EqualValues(t, 1, users)
	require.EqualValues(t, 1, bindings)
	var role string
	require.NoError(t, f.db.Raw("SELECT role FROM tenant_members WHERE user_id=?", f.uid).Scan(&role).Error)
	require.Equal(t, "contributor", role)
}

func TestSIDTLSUserInfoNegativeNoBusinessWrites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*sidTLSFixture)
	}{
		{"issuer", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["iss"] = "https://evil.invalid" } }},
		{"audience", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["aud"] = "other" } }},
		{"azp", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["azp"] = "other" } }},
		{"multiple_audience_no_azp", func(f *sidTLSFixture) {
			f.claimsEdit = func(c jwt.MapClaims) { c["aud"] = []string{f.s.config.OIDCAuth.ClientID, "other"} }
		}},
		{"expired", func(f *sidTLSFixture) {
			f.claimsEdit = func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }
		}},
		{"missing_exp", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { delete(c, "exp") } }},
		{"future_iat", func(f *sidTLSFixture) {
			f.claimsEdit = func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Hour).Unix() }
		}},
		{"old_iat", func(f *sidTLSFixture) {
			f.claimsEdit = func(c jwt.MapClaims) { c["iat"] = time.Now().Add(-time.Hour).Unix() }
		}},
		{"missing_iat", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { delete(c, "iat") } }},
		{"string_iat", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["iat"] = "123" } }},
		{"future_nbf", func(f *sidTLSFixture) {
			f.claimsEdit = func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }
		}},
		{"wrong_nonce", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["nonce"] = "wrong" } }},
		{"missing_nonce", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { delete(c, "nonce") } }},
		{"empty_sub", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["sub"] = "" } }},
		{"numeric_sub", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["sub"] = 123 } }},
		{"wrong_at_hash", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["at_hash"] = "wrong" } }},
		{"wrong_c_hash", func(f *sidTLSFixture) { f.claimsEdit = func(c jwt.MapClaims) { c["c_hash"] = "wrong" } }},
		{"alg_none", func(f *sidTLSFixture) { f.headerEdit = func(h map[string]interface{}) { h["alg"] = "none" } }},
		{"alg_rs512", func(f *sidTLSFixture) { f.headerEdit = func(h map[string]interface{}) { h["alg"] = "RS512" } }},
		{"external_jku", func(f *sidTLSFixture) {
			f.headerEdit = func(h map[string]interface{}) { h["jku"] = "https://evil.invalid/jwks" }
		}},
		{"missing_id_token", func(f *sidTLSFixture) { f.tokenEdit = func(v map[string]interface{}) { delete(v, "id_token") } }},
		{"missing_access_token", func(f *sidTLSFixture) { f.tokenEdit = func(v map[string]interface{}) { delete(v, "access_token") } }},
		{"non_bearer", func(f *sidTLSFixture) { f.tokenEdit = func(v map[string]interface{}) { v["token_type"] = "unknown" } }},
		{"malformed_jwt", func(f *sidTLSFixture) { f.tokenEdit = func(v map[string]interface{}) { v["id_token"] = "invalid.jwt" } }},
		{"token_error_with_tokens", func(f *sidTLSFixture) { f.tokenEdit = func(v map[string]interface{}) { v["error"] = "invalid_grant" } }},
		{"duplicate_response_json", func(f *sidTLSFixture) {
			f.rawTokenJSON = `{"id_token":"one","id_token":"two","access_token":"x","token_type":"Bearer"}`
		}},
		{"token_endpoint_401", func(f *sidTLSFixture) { f.tokenStatus = 401 }},
		{"token_redirect", func(f *sidTLSFixture) { f.tokenStatus = 307 }},
		{"userinfo_500", func(f *sidTLSFixture) { f.profileStatus = 500 }},
		{"userinfo_redirect", func(f *sidTLSFixture) { f.profileStatus = 302 }},
		{"userinfo_subject_mismatch", func(f *sidTLSFixture) { f.profileEdit = func(p map[string]interface{}) { p["sub"] = "other-person" } }},
		{"userinfo_missing_sub", func(f *sidTLSFixture) { f.profileEdit = func(p map[string]interface{}) { delete(p, "sub") } }},
		{"userinfo_no_employee_no_id_token_fallback", func(f *sidTLSFixture) { f.profileEdit = func(p map[string]interface{}) { delete(p, "attributes") } }},
		{"untrusted_tls_certificate", func(f *sidTLSFixture) {
			f.s.protocolClient.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
		}},
		{"tls_skip_verify", func(f *sidTLSFixture) {
			f.s.protocolClient.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
		}},
		{"tls_wrong_server_name", func(f *sidTLSFixture) {
			f.s.protocolClient.Transport.(*http.Transport).TLSClientConfig.ServerName = "other.invalid"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSIDTLSFixture(t)
			f.mu.Lock()
			tc.apply(f)
			f.mu.Unlock()
			ctx, _ := f.begin(t, "negative-code")
			before := sidTLSBusinessCounts(t, f.db)
			_, e := f.s.LoginWithOIDC(ctx, "negative-code", f.s.config.OIDCAuth.RedirectURI, types.TenantProvisioningTenantless)
			require.Error(t, e)
			require.Equal(t, before, sidTLSBusinessCounts(t, f.db))
		})
	}
}

func TestSIDTLSUserInfoProvenanceCannotBeForgedOrReplayed(t *testing.T) {
	for _, scenario := range []string{"no_receipt", "id_token_swap", "access_token_swap", "different_transaction", "different_service", "receipt_replay", "expired_transaction", "changed_policy", "direct_jwt_verifier"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSIDTLSFixture(t)
			c := f.s.config.OIDCAuth
			ctx, _ := f.begin(t, "proof-code")
			response, e := f.s.exchangeOIDCCode(ctx, c, "proof-code", c.RedirectURI)
			require.NoError(t, e)
			before := sidTLSBusinessCounts(t, f.db)
			switch scenario {
			case "no_receipt":
				response.sidTLSProof = nil
			case "id_token_swap":
				response.IDToken += "changed"
			case "access_token_swap":
				response.AccessToken = "other-access"
			case "different_transaction":
				ctx, _ = f.begin(t, "second-code")
			case "different_service":
				copyService := *f.s
				f.s = &copyService
			case "receipt_replay":
				_, e = f.s.resolveOIDCUserInfo(ctx, c, response)
				require.NoError(t, e)
			case "expired_transaction":
				ctx.Value(sidTLSAuthorizationKey{}).(*sidTLSAuthorization).expires = time.Now().Add(-time.Second)
			case "changed_policy":
				c.SourceIDTLSUserInfoApprovalRef = "different-approval"
			case "direct_jwt_verifier":
				_, e = f.s.verifyOIDCIDToken(ctx, c, response.IDToken)
				require.Error(t, e)
				return
			}
			_, e = f.s.resolveOIDCUserInfo(ctx, c, response)
			require.Error(t, e)
			require.Equal(t, before, sidTLSBusinessCounts(t, f.db))
		})
	}
}

func TestSIDTLSUserInfoStateCodeAndConfigurationBinding(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	ctx, state := f.begin(t, "same-code")
	_, e := f.s.PrepareOIDCCallback(context.Background(), state)
	require.Error(t, e)
	_, e = f.s.exchangeOIDCCode(context.Background(), c, "same-code", c.RedirectURI)
	require.Error(t, e)
	require.Zero(t, f.tokenCalls.Load())
	_, e = f.s.exchangeOIDCCode(ctx, c, "same-code", c.RedirectURI)
	require.NoError(t, e)
	_, e = f.s.exchangeOIDCCode(ctx, c, "same-code", c.RedirectURI)
	require.Error(t, e)
	second, _ := f.begin(t, "same-code")
	_, e = f.s.exchangeOIDCCode(second, c, "same-code", c.RedirectURI)
	require.Error(t, e)
	require.EqualValues(t, 1, f.tokenCalls.Load())
	auth, e := f.s.GetOIDCAuthorizationURL(context.Background(), c.RedirectURI)
	require.NoError(t, e)
	c.SourceIDTLSUserInfoApprovalRef = "changed-after-authorize"
	_, e = f.s.PrepareOIDCCallback(context.Background(), auth.State)
	require.Error(t, e)
}

func TestSIDTLSUserInfoSubjectSecurityClaimsNotOverwritten(t *testing.T) {
	f := newSIDTLSFixture(t)
	f.mu.Lock()
	f.profileEdit = func(p map[string]interface{}) {
		p["nonce"] = "untrusted-profile-nonce"
		p["iss"] = "https://profile-cannot-change-issuer.invalid"
		p["exp"] = 1
	}
	f.mu.Unlock()
	ctx, _ := f.begin(t, "security-claims")
	r, e := f.s.exchangeOIDCCode(ctx, f.s.config.OIDCAuth, "security-claims", f.s.config.OIDCAuth.RedirectURI)
	require.NoError(t, e)
	info, e := f.s.resolveOIDCUserInfo(ctx, f.s.config.OIDCAuth, r)
	require.NoError(t, e)
	require.Equal(t, config.SourceIDPinnedIssuer, info.Claims["iss"])
	require.NotEqual(t, "untrusted-profile-nonce", info.Claims["nonce"])
}

func TestSIDTLSUserInfoParallelDifferentTransactions(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	contexts := make([]context.Context, 8)
	codes := []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	for i := range codes {
		contexts[i], _ = f.begin(t, codes[i])
	}
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, e := f.s.LoginWithOIDC(contexts[i], codes[i], c.RedirectURI, types.TenantProvisioningTenantless)
			if e == nil && (!r.Success || r.User.ID != f.uid || r.User.IsSystemAdmin || r.User.CanAccessAllTenants) {
				e = errors.New("parallel login identity or privilege mismatch")
			}
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.EqualValues(t, 8, f.tokenCalls.Load())
	require.EqualValues(t, 8, f.profileCalls.Load())
	require.Equal(t, []int64{1, 1, 8, 8, 8}, sidTLSBusinessCounts(t, f.db))
}

func TestSIDTLSUserInfoUntrustedTransportCannotMintProof(t *testing.T) {
	f := newSIDTLSFixture(t)
	f.s.protocolClient.Transport = &sidPinnedRejectNetwork{}
	ctx, _ := f.begin(t, "custom-transport")
	_, e := f.s.exchangeOIDCCode(ctx, f.s.config.OIDCAuth, "custom-transport", f.s.config.OIDCAuth.RedirectURI)
	require.Error(t, e)
	require.Zero(t, f.tokenCalls.Load())
}

func TestSIDTLSUserInfoNoGlobalSignatureBypass(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	ctx, _ := f.begin(t, "trusted-only")
	r, e := f.s.exchangeOIDCCode(ctx, c, "trusted-only", c.RedirectURI)
	require.NoError(t, e)
	_, e = f.s.resolveOIDCUserInfo(context.Background(), c, &oidcTokenResponse{IDToken: r.IDToken, AccessToken: r.AccessToken, TokenType: "Bearer"})
	require.Error(t, e)
	_, e = f.s.resolveOIDCUserInfo(ctx, c, r)
	require.NoError(t, e)
	copyConfig := *c
	copyConfig.SourceIDValidationStrategy = config.SourceIDJWKSValidation
	copyConfig.JwksURI = ""
	_, e = f.s.verifyOIDCIDToken(ctx, &copyConfig, r.IDToken)
	require.Error(t, e)
	require.False(t, strings.Contains(config.SourceIDJWKSValidation, "tls"))
}

func TestSIDTLSUserInfoConcurrentSameReceiptOnlyOneConsumer(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	ctx, _ := f.begin(t, "shared-receipt")
	r, e := f.s.exchangeOIDCCode(ctx, c, "shared-receipt", c.RedirectURI)
	require.NoError(t, e)
	var passed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := f.s.resolveOIDCUserInfo(ctx, c, r); e == nil {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, passed.Load())
	require.EqualValues(t, 1, f.profileCalls.Load())
}

func TestSIDTLSUserInfoExpiryDuringUserInfoNoWrites(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	c.SourceIDTLSUserInfoExpiresAt = time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)
	f.mu.Lock()
	f.profileDelay = 2500 * time.Millisecond
	f.mu.Unlock()
	ctx, _ := f.begin(t, "expiry-in-flight")
	before := sidTLSBusinessCounts(t, f.db)
	_, e := f.s.LoginWithOIDC(ctx, "expiry-in-flight", c.RedirectURI, types.TenantProvisioningTenantless)
	require.Error(t, e)
	require.Equal(t, before, sidTLSBusinessCounts(t, f.db))
}

func TestSIDTLSUserInfoNewEmployeeJITRemainsUnprivileged(t *testing.T) {
	f := newSIDTLSFixture(t)
	c := f.s.config.OIDCAuth
	for _, name := range []string{"003028_sourceid_enterprise_ownership", "003029_sourceid_jit_registration"} {
		raw, e := os.ReadFile("../../../migrations/sqlite/" + name + ".up.sql")
		require.NoError(t, e)
		require.NoError(t, f.db.Exec(string(raw)).Error)
	}
	require.NoError(t, f.db.AutoMigrate(&types.Tenant{}))
	// The older local-session fixture has a minimal membership table. JIT's
	// existing soft-delete-aware membership check needs this production column.
	require.NoError(t, f.db.Exec("ALTER TABLE tenant_members ADD COLUMN deleted_at datetime").Error)
	c.SourceIDAccountScope = "enterprise_all"
	c.SourceIDPilotAccountsFile = ""
	c.SourceIDEnterprisePolicyConfirmed = true
	c.SourceIDEnterprisePolicyRef = "SYNTHETIC-ENTERPRISE"
	c.SourceIDEnterpriseMaxAge = "60m"
	c.SourceIDJITEnable = true
	c.SourceIDJITPolicyConfirmed = true
	c.SourceIDJITPolicyRef = "SYNTHETIC-JIT"
	c.SourceIDJITEmailDomains = "example.test"
	f.mu.Lock()
	f.info.Subject = "synthetic-new-employee-sub"
	f.info.Claims["attributes"] = map[string]interface{}{"RJGH": "synthetic-new-employee", "RJEMAIL": "synthetic-new@example.test"}
	f.mu.Unlock()
	var firstID string
	for _, code := range []string{"jit-first", "jit-repeat"} {
		ctx, _ := f.begin(t, code)
		result, e := f.s.LoginWithOIDC(ctx, code, c.RedirectURI, types.TenantProvisioningTenantless)
		require.NoError(t, e)
		require.True(t, result.Success)
		require.Zero(t, result.User.TenantID)
		require.False(t, result.User.IsSystemAdmin)
		require.False(t, result.User.CanAccessAllTenants)
		require.Empty(t, result.RefreshToken)
		if firstID == "" {
			firstID = result.User.ID
			require.NotEqual(t, f.uid, firstID)
		} else {
			require.Equal(t, firstID, result.User.ID)
		}
		require.NoError(t, f.s.Logout(context.Background(), result.Token))
	}
	var users, members int64
	require.NoError(t, f.db.Model(&types.User{}).Count(&users).Error)
	require.EqualValues(t, 2, users)
	require.NoError(t, f.db.Table("tenant_members").Where("user_id=?", firstID).Count(&members).Error)
	require.Zero(t, members)
}
