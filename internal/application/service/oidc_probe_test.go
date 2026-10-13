package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCProtocolProbe(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"success", "approved_callback", "basic", "rs384", "signature", "issuer", "audience", "expiry", "nonce", "subject", "userinfo_failure", "token_failure", "token_oauth_error", "token_unknown_error", "token_invalid_json", "token_redirect", "jwks_redirect", "userinfo_redirect", "provider_error"} {
		t.Run(kind, func(t *testing.T) {
			withOIDCSSRFWhitelist(t, "127.0.0.1")
			const private = "synthetic-private-probe-value"
			var nonce, challenge string
			var fixtureMu sync.Mutex
			var calls atomic.Int32
			expectedRedirectURI := "http://probe.example/probe/callback"
			if kind == "approved_callback" {
				expectedRedirectURI = approvedSourceIDProbeCallbackURI
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					fixtureMu.Lock()
					expectedNonce, expectedChallenge := nonce, challenge
					fixtureMu.Unlock()
					calls.Add(1)
					if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Error("unsafe token transport")
					}
					_ = r.ParseForm()
					if r.Form.Get("redirect_uri") != expectedRedirectURI {
						t.Error("approved redirect_uri changed during exchange")
					}
					if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != private || r.Form.Get("client_id") != "probe-client" {
						t.Error("wrong code exchange")
					}
					if kind == "basic" {
						username, password, ok := r.BasicAuth()
						if !ok || username != "probe-client" || password != private || r.Form.Get("client_secret") != "" {
							t.Error("wrong basic auth")
						}
					} else if r.Form.Get("client_secret") != private || r.Header.Get("Authorization") != "" {
						t.Error("missing post auth")
					}
					sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if base64.RawURLEncoding.EncodeToString(sum[:]) != expectedChallenge {
						t.Error("PKCE not bound")
					}
					if kind == "token_redirect" {
						http.Redirect(w, r, "/should-not-follow", http.StatusTemporaryRedirect)
						return
					}
					if kind == "token_failure" {
						w.WriteHeader(401)
						_, _ = w.Write([]byte(private))
						return
					}
					if kind == "token_oauth_error" || kind == "token_unknown_error" {
						errorCode := "invalid_client"
						if kind == "token_unknown_error" {
							errorCode = private
							w.Header().Set("X-Request-Id", private)
						}
						w.WriteHeader(401)
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": errorCode, "error_description": private, "client_secret": private, "access_token": private})
						return
					}
					if kind == "token_invalid_json" {
						_, _ = w.Write([]byte(private))
						return
					}
					claims := baseClaims("https://confirmed.example", "probe-client")
					claims["nonce"] = expectedNonce
					claims["employee_candidate"] = private
					if kind == "issuer" {
						claims["iss"] = "https://wrong.example"
					}
					if kind == "audience" {
						claims["aud"] = "wrong-client"
					}
					if kind == "expiry" {
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					}
					if kind == "nonce" {
						claims["nonce"] = "wrong"
					}
					id := signedIDToken(t, key, "probe-key", claims)
					if kind == "rs384" {
						token := jwt.NewWithClaims(jwt.SigningMethodRS384, jwt.MapClaims(claims))
						token.Header["kid"] = "probe-key"
						signed, signErr := token.SignedString(key)
						if signErr != nil {
							t.Fatal("could not sign RSA algorithm rejection fixture")
						}
						id = signed
					}
					if kind == "signature" {
						id = unsignedJWT(claims)
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"id_token": id, "access_token": private, "refresh_token": private, "token_type": "Bearer"})
				case "/jwks":
					if kind == "rs384" {
						t.Error("rejected algorithm reached JWKS")
					}
					if kind == "jwks_redirect" {
						http.Redirect(w, r, "/should-not-follow", http.StatusFound)
						return
					}
					_ = json.NewEncoder(w).Encode(oidcJWKS{Keys: []oidcJWK{rsaJWK(key, "probe-key")}})
				case "/userinfo":
					if kind == "rs384" {
						t.Error("rejected algorithm reached UserInfo")
					}
					if kind == "userinfo_redirect" {
						http.Redirect(w, r, "/should-not-follow", http.StatusFound)
						return
					}
					if r.Header.Get("Authorization") != "Bearer "+private {
						t.Error("missing userinfo bearer")
					}
					if kind == "userinfo_failure" {
						w.WriteHeader(500)
						_, _ = w.Write([]byte(private))
						return
					}
					sub := "user-123"
					if kind == "subject" {
						sub = "other-subject"
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": sub, "employee_candidate": private, "attributes": map[string]interface{}{"status": true, "nested": map[string]interface{}{"private_field": private}}, "items": []interface{}{private}})
				case "/should-not-follow":
					t.Error("probe followed provider redirect")
					w.WriteHeader(500)
				default:
					w.WriteHeader(404)
				}
			}))
			defer provider.Close()
			m := OIDCProbeMetadata{Confirmed: true, Issuer: "https://confirmed.example", Authorization: "https://confirmed.example/authorize", Token: provider.URL + "/token", JWKS: provider.URL + "/jwks", UserInfo: provider.URL + "/userinfo", RedirectURI: "http://probe.example/probe/callback", ClientID: "probe-client", ClientAuth: "client_secret_post", UserInfoTransport: "bearer_get", Scopes: []string{"openid"}, PKCE: true}
			if kind == "basic" {
				m.ClientAuth = "client_secret_basic"
			}
			m.RedirectURI = expectedRedirectURI
			// Only test fixtures bypass production HTTPS validation. No repository exists.
			p := newOIDCProtocolProbe(m, private)
			start := httptest.NewRecorder()
			p.ServeHTTP(start, httptest.NewRequest("GET", "/probe/start", nil))
			u, err := url.Parse(start.Header().Get("Location"))
			if err != nil || start.Code != 302 {
				t.Fatal("authorize failed")
			}
			q := u.Query()
			if q.Get("redirect_uri") != expectedRedirectURI {
				t.Fatal("approved redirect_uri changed during authorize")
			}
			fixtureMu.Lock()
			nonce = q.Get("nonce")
			challenge = q.Get("code_challenge")
			fixtureMu.Unlock()
			originalVerifier := p.verifier
			if strings.Contains(start.Body.String(), private) || start.Header().Get("Set-Cookie") != "" || q.Get("scope") != "openid" {
				t.Fatal("authorize leaked or changed scope")
			}
			bad := httptest.NewRecorder()
			if kind == "approved_callback" {
				p.ServeHTTP(bad, httptest.NewRequest("GET", "/probe/callback?state="+q.Get("state")+"&code="+private, nil))
				if bad.Code != 404 || calls.Load() != 0 {
					t.Fatal("unconfigured path reached provider")
				}
				bad = httptest.NewRecorder()
			}
			p.ServeHTTP(bad, httptest.NewRequest("GET", p.callbackPath()+"?state=wrong&code="+private, nil))
			if bad.Code != 400 || calls.Load() != 0 {
				t.Fatal("invalid state reached provider")
			}
			callback := url.Values{"state": {q.Get("state")}, "code": {private}}
			if kind == "provider_error" {
				callback.Set("error", private)
				callback.Del("code")
			}
			result := httptest.NewRecorder()
			p.ServeHTTP(result, httptest.NewRequest("GET", p.callbackPath()+"?"+callback.Encode(), nil))
			completed := kind == "success" || kind == "approved_callback" || kind == "basic"
			if p.Report().Validation["completed"] != completed {
				t.Fatalf("unexpected validation result for %s", kind)
			}
			if kind != "provider_error" {
				d := p.Report().HTTP[0]
				if d.Stage != "token" || d.ClientAuth != m.ClientAuth || d.AuthorizationHeaderPresent == nil || *d.AuthorizationHeaderPresent != (kind == "basic") {
					t.Fatal("client authentication placement diagnostics incorrect")
				}
				if d.CallbackToTokenDispatchMS == nil || *d.CallbackToTokenDispatchMS < 0 || (d.CallbackToTokenSentMS != nil && *d.CallbackToTokenSentMS < *d.CallbackToTokenDispatchMS) {
					t.Fatal("callback-to-dispatch/write timing missing or out of order")
				}
			}
			if kind == "rs384" && (result.Code != 400 || p.Report().Validation["algorithm"] || p.Report().Claims != nil || p.Report().Algorithm != "") {
				t.Fatal("non-RS256 token was accepted or exposed claims")
			}
			if kind == "token_failure" || kind == "token_oauth_error" || kind == "token_unknown_error" || kind == "token_invalid_json" {
				diagnostics := p.Report().HTTP
				if len(diagnostics) != 1 || diagnostics[0].Stage != "token" || diagnostics[0].DurationMS < 0 {
					t.Fatal("token failure lost bounded diagnostics")
				}
				wantStatus, wantFailure := 401, "http_status"
				if kind == "token_invalid_json" {
					wantStatus, wantFailure = 200, "invalid_response_json"
				}
				if diagnostics[0].Status != wantStatus || diagnostics[0].Failure != wantFailure {
					t.Fatal("incorrect failure classification")
				}
				if (diagnostics[0].OAuthError == "invalid_client") != (kind == "token_oauth_error") {
					t.Fatal("OAuth error allowlist lost or exposed unknown value")
				}
			}
			encoded, _ := json.Marshal(p.Report())
			for _, sensitive := range []string{private, nonce, q.Get("state"), originalVerifier, "user-123", "user@example.com", "Real User"} {
				if strings.Contains(string(encoded), sensitive) {
					t.Fatal("sensitive protocol value reached report")
				}
			}
			if kind == "subject" && (p.Report().UserInfo == nil || p.Report().Validation["userinfo_subject"]) {
				t.Fatal("subject mismatch lost safe schema or passed identity check")
			}
			if strings.Contains(string(encoded), private) || strings.Contains(result.Body.String(), private) || result.Header().Get("Set-Cookie") != "" {
				t.Fatal("credential or personal value leaked")
			}
			if completed && (p.Report().Algorithm != "RS256" || p.Report().Claims["employee_candidate"] != "string" || p.Report().UserInfo["employee_candidate"] != "string") {
				t.Fatal("safe schema missing")
			}
			select {
			case <-p.Done():
			default:
				t.Fatal("probe did not close")
			}
			replay := httptest.NewRecorder()
			p.ServeHTTP(replay, httptest.NewRequest("GET", p.callbackPath()+"?"+callback.Encode(), nil))
			if replay.Code != 410 {
				t.Fatal("closed probe accepted replay")
			}
			if p.cfg.ClientSecret != "" || p.state != "" || p.nonce != "" || p.verifier != "" {
				t.Fatal("transaction retained after completion")
			}
			if kind == "provider_error" && calls.Load() != 0 {
				t.Fatal("provider error exchanged code")
			}
		})
	}
}

type probeTraceRoundTripper func(*http.Request) (*http.Response, error)

func (f probeTraceRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type probeTimeoutError struct{}

func (probeTimeoutError) Error() string   { return "synthetic-private-transport-cause" }
func (probeTimeoutError) Timeout() bool   { return true }
func (probeTimeoutError) Temporary() bool { return false }

func TestOIDCProbeSafeTransportCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"timeout", probeTimeoutError{}, "timeout"},
		{"certificate", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "synthetic-private-transport-cause"}, "tls_error"},
		{"network", errors.New("synthetic-private-transport-cause"), "transport_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := OIDCProbeReport{}
			endpoint := "https://confirmed.example/token"
			transport := probeDiagnosticTransport{base: probeTraceRoundTripper(func(*http.Request) (*http.Response, error) { return nil, tc.err }), endpoints: map[string]string{"token": endpoint}, report: &report, callbackReceived: time.Now(), clientAuth: "client_secret_post"}
			req, _ := http.NewRequest("POST", endpoint, nil)
			_, err := transport.RoundTrip(req)
			if err == nil || len(report.HTTP) != 1 || report.HTTP[0].Failure != tc.want || report.HTTP[0].Status != 0 || report.HTTP[0].CallbackToTokenSentMS != nil {
				t.Fatal("transport failure category incorrect or write falsely recorded")
			}
			encoded, _ := json.Marshal(report)
			if strings.Contains(string(encoded), "synthetic-private-transport-cause") {
				t.Fatal("raw transport/certificate detail reached report")
			}
		})
	}
}

func TestOIDCProbeCallbackWriteTiming(t *testing.T) {
	report := OIDCProbeReport{}
	endpoint := "https://confirmed.example/token"
	transport := probeDiagnosticTransport{
		base: probeTraceRoundTripper(func(r *http.Request) (*http.Response, error) {
			trace := httptrace.ContextClientTrace(r.Context())
			if trace == nil || trace.WroteRequest == nil {
				t.Fatal("missing request-write timing hook")
			}
			trace.WroteRequest(httptrace.WroteRequestInfo{})
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_client"}`))}, nil
		}), endpoints: map[string]string{"token": endpoint}, report: &report,
		callbackReceived: time.Now().Add(-30 * time.Millisecond), clientAuth: "client_secret_post",
	}
	req, _ := http.NewRequest("POST", endpoint, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal("timing fixture failed")
	}
	resp.Body.Close()
	d := report.HTTP[0]
	if d.CallbackToTokenDispatchMS == nil || d.CallbackToTokenSentMS == nil || *d.CallbackToTokenDispatchMS < 30 || *d.CallbackToTokenSentMS < *d.CallbackToTokenDispatchMS {
		t.Fatal("write timing did not include callback handling interval")
	}
}

func TestOIDCProbePostCredentialEncoding(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	const secret = "synthetic secret &+=:/"
	const code = "synthetic-code&+=:/"
	const verifier = "synthetic-verifier"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("post credential transport leaked or changed")
		}
		_ = r.ParseForm()
		if r.PostForm.Get("client_id") != "probe-client" || r.PostForm.Get("client_secret") != secret || r.PostForm.Get("code") != code || r.PostForm.Get("redirect_uri") != approvedSourceIDProbeCallbackURI || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code_verifier") != verifier {
			t.Error("post fields failed exact encoding round-trip")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "synthetic-access", "id_token": "synthetic-id"})
	}))
	defer server.Close()
	svc := &userService{protocolClient: server.Client()}
	cfg := config.OIDCAuthConfig{TokenEndpoint: server.URL, ClientID: "probe-client", ClientSecret: secret, ClientAuthMethod: "client_secret_post"}
	ctx := context.WithValue(context.Background(), oidcVerifierKey, verifier)
	if _, err := svc.exchangeOIDCCode(ctx, &cfg, code, approvedSourceIDProbeCallbackURI); err != nil {
		t.Fatal("post exchange fixture failed")
	}
}

func TestOIDCProbeRequiresConfirmedIndependentMetadata(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.example")
	m := OIDCProbeMetadata{Confirmed: true, Issuer: "https://sid.example", Authorization: "https://sid.example/authorize", Token: "https://sid.example/token", JWKS: "https://sid.example/jwks", UserInfo: "https://sid.example/userinfo", RedirectURI: "http://test.example/probe/callback", ClientID: "knowhub", ClientAuth: "client_secret_post", UserInfoTransport: "bearer_get", Scopes: []string{"openid"}}
	valid, err := NewOIDCProtocolProbe(m, "synthetic")
	if err != nil {
		t.Fatal("valid metadata rejected")
	}
	valid.Close()
	approved := m
	approved.RedirectURI = approvedSourceIDProbeCallbackURI
	if ValidateOIDCProbeMetadata(approved) != nil {
		t.Fatal("exact approved callback rejected")
	}
	for _, uri := range []string{
		approvedSourceIDProbeCallbackURI + "/", approvedSourceIDProbeCallbackURI + "?probe=1",
		strings.Replace(approvedSourceIDProbeCallbackURI, "http:", "https:", 1),
		strings.Replace(approvedSourceIDProbeCallbackURI, "knowhub.ruijie.com.cn", "knowhub.ruijie.com.cn:80", 1),
	} {
		c := approved
		c.RedirectURI = uri
		if ValidateOIDCProbeMetadata(c) == nil {
			t.Fatal("modified approved callback accepted")
		}
	}
	for _, kind := range []string{"unconfirmed", "missing", "http", "query_secret", "business_callback", "offline", "auth_method"} {
		t.Run(kind, func(t *testing.T) {
			c := m
			switch kind {
			case "unconfirmed":
				c.Confirmed = false
			case "missing":
				c.JWKS = ""
			case "http":
				c.Token = "http://sid.example/token"
			case "query_secret":
				c.Token += "?client_secret=synthetic"
			case "business_callback":
				c.RedirectURI = "http://test.example/api/v1/auth/oidc/callback"
			case "offline":
				c.Scopes = []string{"openid", "offline_access"}
			case "auth_method":
				c.ClientAuth = ""
			}
			if _, err := NewOIDCProtocolProbe(c, "synthetic"); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}

func TestOIDCProbeExpiryAndDuplicateParameters(t *testing.T) {
	p := newOIDCProtocolProbe(OIDCProbeMetadata{Authorization: "https://sid.example/authorize"}, "synthetic")
	start := httptest.NewRecorder()
	p.ServeHTTP(start, httptest.NewRequest("GET", "/probe/start", nil))
	u, _ := url.Parse(start.Header().Get("Location"))
	state := u.Query().Get("state")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "/probe/callback?state="+state+"&state="+state+"&code=synthetic", nil))
	if w.Code != 400 {
		t.Fatal("duplicate state accepted")
	}
	p.deadline = time.Now().Add(-time.Second)
	w = httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "/probe/callback?state="+state+"&code=synthetic", nil))
	if w.Code != 410 || p.cfg.ClientSecret != "" {
		t.Fatal("expired probe retained credential")
	}
}
