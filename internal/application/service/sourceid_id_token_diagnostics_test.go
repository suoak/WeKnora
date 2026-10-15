package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

func TestSourceIDIDTokenFailureSyntheticBranches(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := jwksServer(t, key, "synthetic-key")
	defer server.Close()
	cfg := &config.OIDCAuthConfig{SourceIDEnable: true, IssuerURL: "https://synthetic.example", ClientID: "knowhub", JwksURI: server.URL}
	for _, tc := range []struct{ name, category string }{
		{"parse", "parse"}, {"algorithm", "algorithm"}, {"signature", "signature"},
		{"key", "key_selection"}, {"issuer", "issuer"}, {"audience", "audience"},
		{"exp", "expired"}, {"iat", "iat"}, {"nbf", "nbf"}, {"missing_iat", "iat_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := baseClaims(cfg.IssuerURL, cfg.ClientID)
			method := jwt.SigningMethodRS256
			signingKey := key
			switch tc.name {
			case "algorithm":
				method = jwt.SigningMethodRS384
			case "signature":
				signingKey = wrongKey
			case "issuer":
				claims["iss"] = "https://other.synthetic.example"
			case "audience":
				claims["aud"] = "other-client"
			case "exp":
				claims["exp"] = time.Now().Add(-10 * time.Minute).Unix()
			case "iat":
				claims["iat"] = time.Now().Add(10 * time.Minute).Unix()
			case "nbf":
				claims["nbf"] = time.Now().Add(10 * time.Minute).Unix()
			case "missing_iat":
				delete(claims, "iat")
			}
			token := jwt.NewWithClaims(method, claims)
			token.Header["kid"] = "synthetic-key"
			if tc.name == "key" {
				token.Header["kid"] = "unknown-synthetic-key"
			}
			raw, err := token.SignedString(signingKey)
			if err != nil {
				t.Fatal("synthetic signing failed")
			}
			if tc.name == "parse" {
				raw = "synthetic-malformed"
			}
			_, err = (&userService{}).verifyOIDCIDToken(context.Background(), cfg, raw)
			if err == nil || !strings.Contains(strings.Join(sourceIDIDTokenFailureCategories(err), ","), tc.category) {
				t.Fatalf("expected category %s", tc.category)
			}
		})
	}
}

func TestSourceIDJWKSNoKidNodeDifferenceFailsClosed(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	keyA, _ := rsa.GenerateKey(rand.Reader, 2048)
	keyB, _ := rsa.GenerateKey(rand.Reader, 2048)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := keyA
		if calls.Add(1)%2 == 0 {
			key = keyB
		}
		_ = json.NewEncoder(w).Encode(oidcJWKS{Keys: []oidcJWK{rsaJWK(key, "")}})
	}))
	defer server.Close()
	cfg := &config.OIDCAuthConfig{SourceIDEnable: true, IssuerURL: "https://synthetic.example", ClientID: "knowhub", JwksURI: server.URL}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, baseClaims(cfg.IssuerURL, cfg.ClientID))
	raw, err := token.SignedString(keyA)
	if err != nil {
		t.Fatal("synthetic signing failed")
	}
	s := &userService{}
	if _, err := s.verifyOIDCIDToken(context.Background(), cfg, raw); err != nil {
		t.Fatal("single matching no-kid key rejected")
	}
	if _, err := s.verifyOIDCIDToken(context.Background(), cfg, raw); err == nil || !reflect.DeepEqual(sourceIDIDTokenFailureCategories(err), []string{"signature"}) {
		t.Fatal("different node signing key not rejected")
	}
	if calls.Load() != 2 {
		t.Fatal("JWKS fetch unexpectedly cached or retried")
	}
	// A JWT-provided kid never falls back to an unrelated key lacking kid.
	token.Header["kid"] = "synthetic-unmatched"
	raw, err = token.SignedString(keyA)
	if err != nil {
		t.Fatal("synthetic signing failed")
	}
	if _, err = s.verifyOIDCIDToken(context.Background(), cfg, raw); err == nil || !reflect.DeepEqual(sourceIDIDTokenFailureCategories(err), []string{"key_selection"}) {
		t.Fatal("unmatched kid was accepted")
	}
}

func TestSourceIDIDTokenFailureDiagnosticsPrivateAndDisabled(t *testing.T) {
	var out bytes.Buffer
	log := logrus.New()
	log.SetOutput(&out)
	ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
	err := errors.New("SyntheticSecret SyntheticToken employee@example.invalid")
	sourceIDIDTokenFailureDiagnostic(ctx, err)
	if out.Len() != 0 {
		t.Fatal("disabled diagnostic emitted")
	}
	ctx = NewSourceIDDiagnosticContext(ctx, true)
	sourceIDIDTokenFailureDiagnostic(ctx, err)
	if !bytes.Contains(out.Bytes(), []byte("sourceid_id_token_failure")) || bytes.Contains(out.Bytes(), []byte("Synthetic")) || bytes.Contains(out.Bytes(), []byte("employee@")) {
		t.Fatal("unsafe or absent failure diagnostic")
	}
	for message, category := range map[string]string{"OIDC ID Token nonce mismatch": "nonce", "missing nonce": "nonce_missing", "id_token authorized party mismatch": "authorized_party", "id_token missing sub claim": "subject_missing"} {
		if !reflect.DeepEqual(sourceIDIDTokenFailureCategories(errors.New(message)), []string{category}) {
			t.Fatal("code-owned error classification mismatch")
		}
	}
}
