package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSourceIDProtocolOnlyVerifiesIdentityBeforeRejectingBusinessLogin(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gate.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	defer func(cleanup func() error) { _ = cleanup() }(connection.Close)
	if err = db.AutoMigrate(
		&types.OIDCTransaction{},
		&types.User{},
		&types.AuthToken{},
		&types.ExternalIdentity{},
		&types.SourceIDSession{},
	); err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := jwksServer(t, key, "mock")
	defer jwks.Close()
	nonce := ""
	calls := 0
	profileCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/profile" {
			profileCalls++
			_ = json.NewEncoder(w).
				Encode(map[string]interface{}{"sub": "user-123", "employee_candidate": "synthetic-private"})
			return
		}
		calls++
		_ = r.ParseForm()
		id, _, ok := r.BasicAuth()
		if !ok || id != "knowhub" || r.PostForm.Has("client_secret") || r.PostForm.Has("code_verifier") {
			t.Error("Basic/non-PKCE protocol changed")
		}
		claims := baseClaims("https://synthetic.example", "knowhub")
		claims["nonce"] = nonce
		_ = json.NewEncoder(w).
			Encode(oidcTokenResponse{AccessToken: "synthetic-access", IDToken: signedIDToken(t, key, "mock", claims)})
	}))
	defer server.Close()
	cfg := &config.OIDCAuthConfig{
		Enable:                true,
		SourceIDEnable:        true,
		SourceIDProtocolOnly:  true,
		ClientAuthMethod:      "client_secret_basic",
		ClientID:              "knowhub",
		ClientSecret:          "synthetic-secret",
		IssuerURL:             "https://synthetic.example",
		AuthorizationEndpoint: server.URL,
		TokenEndpoint:         server.URL,
		JwksURI:               jwks.URL,
		UserInfoEndpoint:      server.URL + "/profile",
		RedirectURI:           approvedSourceIDProbeCallbackURI,
		Scopes:                []string{"openid"},
	}
	svc := &userService{
		config:    &config.Config{OIDCAuth: cfg},
		userRepo:  apprepo.NewUserRepository(db),
		tokenRepo: apprepo.NewAuthTokenRepository(db),
	}
	auth, err := svc.GetOIDCAuthorizationURL(context.Background(), cfg.RedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	nonce = auth.Nonce
	ctx, err := svc.PrepareOIDCCallback(context.Background(), auth.State)
	if err != nil {
		t.Fatal(err)
	}
	response, err := svc.LoginWithOIDC(ctx, "synthetic-code", cfg.RedirectURI, "")
	if response != nil || err == nil || err.Error() != "sourceid identity contract pending" || calls != 1 ||
		profileCalls != 1 {
		t.Fatal("protocol-only did not reach verified identity gate")
	}
	for _, table := range []string{"users", "auth_tokens", "external_identities", "source_id_sessions"} {
		var count int64
		if db.Table(table).Count(&count).Error != nil || count != 0 {
			t.Fatal("protocol-only wrote business identity or session")
		}
	}
}
