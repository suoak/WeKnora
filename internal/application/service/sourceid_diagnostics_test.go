package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/golang-jwt/jwt/v5"
)

func TestSourceIDBusinessDiagnosticDoesNotEchoPrivateError(t *testing.T) {
	var out bytes.Buffer
	logger.SetOutput(&out)
	defer logger.SetOutput(io.Discard)
	ctx := NewSourceIDDiagnosticContext(context.Background(), true)
	SourceIDDiagnosticStage(
		ctx,
		"token",
		time.Now(),
		errors.New("synthetic-secret synthetic-code synthetic-token "+
			"personal@example.invalid"),
	)
	if bytes.Contains(out.Bytes(), []byte("synthetic-")) ||
		bytes.Contains(out.Bytes(), []byte("personal@")) ||
		!bytes.Contains(out.Bytes(), []byte(`"category":"rejected"`)) {
		t.Fatal("private diagnostic error leaked")
	}
	before := out.Len()
	SourceIDDiagnosticStage(ctx, "synthetic-secret", time.Now(), nil)
	SourceIDDiagnosticStage(context.Background(), "token", time.Now(), nil)
	if out.Len() != before {
		t.Fatal("disabled or unknown stage emitted")
	}
}

func TestSourceIDBusinessRejectsNonRS256(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := jwksServer(t, key, "key")
	defer server.Close()
	cfg := &config.OIDCAuthConfig{
		SourceIDEnable: true,
		IssuerURL:      "https://synthetic.example",
		ClientID:       "knowhub",
		JwksURI:        server.URL,
	}
	for _, method := range []*jwt.SigningMethodRSA{
		jwt.SigningMethodRS256,
		jwt.SigningMethodRS384,
		jwt.SigningMethodRS512,
	} {
		token := jwt.NewWithClaims(method, baseClaims(cfg.IssuerURL, cfg.ClientID))
		token.Header["kid"] = "key"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&userService{}).verifyOIDCIDToken(context.Background(), cfg, raw)
		if (err == nil) != (method == jwt.SigningMethodRS256) {
			t.Fatal("SourceID algorithm policy failed")
		}
	}
}
