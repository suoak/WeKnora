package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/sirupsen/logrus"
)

func TestSourceIDRawBasicExchangeModes(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	for _, tc := range []struct {
		name, mode string
		sourceID   bool
	}{
		{"default", "", true},
		{"oauth_form", "oauth_form", true},
		{"raw", "raw", true},
		{"other_provider_raw_ignored", "raw", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, status := range []int{200, 401} {
				var calls atomic.Int64
				const secret = "Synthetic+/=:% secret"
				rawMode := tc.sourceID && tc.mode == "raw"
				expectedSecret := url.QueryEscape(secret)
				if rawMode {
					expectedSecret = secret
				}
				expectedAuth := "Basic " + base64.StdEncoding.EncodeToString(
					[]byte("knowhub:"+expectedSecret),
				)
				server := httptest.NewTLSServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						if r.Method != "POST" || len(r.Header.Values("Authorization")) != 1 ||
							r.Header.Get("Authorization") != expectedAuth ||
							r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
							t.Error("Basic byte format or request changed")
						}
						if err := r.ParseForm(); err != nil {
							t.Error("form parse failed")
						}
						if len(r.PostForm) != 4 || r.PostForm.Get("grant_type") != "authorization_code" ||
							r.PostForm.Get("client_id") != "knowhub" ||
							r.PostForm.Get("code") != "SyntheticOnlyCode" ||
							r.PostForm.Get(
								"redirect_uri",
							) != "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback" {
							t.Error("body changed")
						}
						if status == 401 {
							w.Header().Set("WWW-Authenticate", "Basic")
							w.WriteHeader(401)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(
							[]byte("{\"access_token\":\"SYNTHETIC_ACCESS\",\"id_token\":\"SYNTHETIC_ID\"}"),
						)
					}),
				)
				var output bytes.Buffer
				log := logrus.New()
				log.SetOutput(&output)
				log.SetFormatter(&logrus.JSONFormatter{})
				ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(log))
				ctx = SourceIDDiagnosticStatePassed(NewSourceIDDiagnosticContext(ctx, tc.sourceID))
				cfg := &config.OIDCAuthConfig{
					SourceIDEnable:            tc.sourceID,
					SourceIDDiagnosticsEnable: true,
					SourceIDBasicEncoding:     tc.mode,
					ClientID:                  "knowhub",
					ClientSecret:              secret,
					ClientAuthMethod:          "client_secret_basic",
					TokenEndpoint:             server.URL,
				}
				s := &userService{protocolClient: server.Client()}
				result, err := s.exchangeOIDCCode(
					ctx,
					cfg,
					"SyntheticOnlyCode",
					"http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback",
				)
				server.Close()
				if calls.Load() != 1 || (status == 200 && (err != nil || result == nil)) ||
					(status == 401 && (err == nil || result != nil)) {
					t.Fatal("result or single exchange changed")
				}
				for _, canary := range []string{
					secret,

					url.QueryEscape(
						secret,
					),

					expectedAuth,

					strings.TrimPrefix(
						expectedAuth,

						"Basic ",
					),

					"SyntheticOnlyCode",

					"SYNTHETIC_ACCESS",

					"SYNTHETIC_ID",
				} {
					if strings.Contains(output.String(), canary) {
						t.Fatal("private value leaked")
					}
				}
				if tc.sourceID {
					strategy := "oauth_form_escape_then_set_basic_auth_base64_once"
					if rawMode {
						strategy = "raw_then_set_basic_auth_base64_once"
					}
					if !strings.Contains(output.String(), strategy) {
						t.Fatal("diagnostic mode missing")
					}
				} else if output.Len() != 0 {
					t.Fatal("other provider diagnostics changed")
				}
			}
		})
	}
}
