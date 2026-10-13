package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCProtocolProbe(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"success", "basic", "signature", "issuer", "audience", "expiry", "nonce", "subject", "userinfo_failure", "token_failure", "token_redirect", "jwks_redirect", "userinfo_redirect", "provider_error"} {
		t.Run(kind, func(t *testing.T) {
			withOIDCSSRFWhitelist(t, "127.0.0.1")
			const private = "synthetic-private-probe-value"
			var nonce, challenge string
			var fixtureMu sync.Mutex
			var calls atomic.Int32
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
					if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != private {
						t.Error("wrong code exchange")
					}
					if kind == "basic" {
						_, password, ok := r.BasicAuth()
						if !ok || password != private || r.Form.Get("client_secret") != "" {
							t.Error("wrong basic auth")
						}
					} else if r.Form.Get("client_secret") != private {
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
					if kind == "signature" {
						id = unsignedJWT(claims)
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"id_token": id, "access_token": private, "refresh_token": private, "token_type": "Bearer"})
				case "/jwks":
					if kind == "jwks_redirect" {
						http.Redirect(w, r, "/should-not-follow", http.StatusFound)
						return
					}
					_ = json.NewEncoder(w).Encode(oidcJWKS{Keys: []oidcJWK{rsaJWK(key, "probe-key")}})
				case "/userinfo":
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
			// Only test fixtures bypass production HTTPS validation. No repository exists.
			p := newOIDCProtocolProbe(m, private)
			start := httptest.NewRecorder()
			p.ServeHTTP(start, httptest.NewRequest("GET", "/probe/start", nil))
			u, err := url.Parse(start.Header().Get("Location"))
			if err != nil || start.Code != 302 {
				t.Fatal("authorize failed")
			}
			q := u.Query()
			fixtureMu.Lock()
			nonce = q.Get("nonce")
			challenge = q.Get("code_challenge")
			fixtureMu.Unlock()
			if strings.Contains(start.Body.String(), private) || start.Header().Get("Set-Cookie") != "" || q.Get("scope") != "openid" {
				t.Fatal("authorize leaked or changed scope")
			}
			bad := httptest.NewRecorder()
			p.ServeHTTP(bad, httptest.NewRequest("GET", "/probe/callback?state=wrong&code="+private, nil))
			if bad.Code != 400 || calls.Load() != 0 {
				t.Fatal("invalid state reached provider")
			}
			callback := url.Values{"state": {q.Get("state")}, "code": {private}}
			if kind == "provider_error" {
				callback.Set("error", private)
				callback.Del("code")
			}
			result := httptest.NewRecorder()
			p.ServeHTTP(result, httptest.NewRequest("GET", "/probe/callback?"+callback.Encode(), nil))
			completed := kind == "success" || kind == "basic"
			if p.Report().Validation["completed"] != completed {
				t.Fatalf("unexpected validation result for %s", kind)
			}
			encoded, _ := json.Marshal(p.Report())
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
			p.ServeHTTP(replay, httptest.NewRequest("GET", "/probe/callback?"+callback.Encode(), nil))
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

func TestOIDCProbeRequiresConfirmedIndependentMetadata(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.example")
	m := OIDCProbeMetadata{Confirmed: true, Issuer: "https://sid.example", Authorization: "https://sid.example/authorize", Token: "https://sid.example/token", JWKS: "https://sid.example/jwks", UserInfo: "https://sid.example/userinfo", RedirectURI: "http://test.example/probe/callback", ClientID: "knowhub", ClientAuth: "client_secret_post", UserInfoTransport: "bearer_get", Scopes: []string{"openid"}}
	valid, err := NewOIDCProtocolProbe(m, "synthetic")
	if err != nil {
		t.Fatal("valid metadata rejected")
	}
	valid.Close()
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
