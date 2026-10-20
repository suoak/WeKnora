package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestSourceIDTokenExchangeDiagnosticsReadOnlyIntegration(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error("invalid form")
				}
				u, p, ok := r.BasicAuth()
				if !ok || u != url.QueryEscape("knowhub") || p != url.QueryEscape("Synthetic+/=:% secret") {
					t.Error("Basic semantics changed")
				}
				if r.Form.Get("code") != "SyntheticOnlyCode" ||
					r.Form.Get(
						"redirect_uri",
					) != "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback" ||
					r.Form.Get("grant_type") != "authorization_code" ||
					r.Form.Get("client_id") != "knowhub" ||
					r.Form.Has("client_secret") ||
					r.Form.Has("code_verifier") {
					t.Error("form semantics changed")
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="synthetic-only"`)
				w.WriteHeader(401)
			}))
			defer server.Close()
			s := &userService{protocolClient: server.Client()}
			cfg := &config.OIDCAuthConfig{
				Enable:                    true,
				SourceIDEnable:            true,
				SourceIDDiagnosticsEnable: enabled,
				SourceIDProtocolOnly:      true,
				ClientID:                  "knowhub",
				ClientSecret:              "Synthetic+/=:% secret",
				ClientAuthMethod:          "client_secret_basic",
				TokenEndpoint:             server.URL,
				PKCEEnable:                false,
			}
			// Capture the actual logger output using a context-owned logger;
			// never change the shared test/process logging configuration.
			var output bytes.Buffer
			captured := logrus.New()
			captured.SetOutput(&output)
			captured.SetFormatter(&logrus.JSONFormatter{})
			ctx := context.WithValue(
				context.Background(),
				types.LoggerContextKey,
				logrus.NewEntry(captured),
			)
			ctx = SourceIDDiagnosticStatePassed(NewSourceIDDiagnosticContext(ctx, enabled))
			result, err := s.exchangeOIDCCode(
				ctx,
				cfg,
				"SyntheticOnlyCode",
				"http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback",
			)
			if result != nil || err == nil || !strings.Contains(err.Error(), "status=401") ||
				calls.Load() != 1 {
				t.Fatal("enable/disable changed rejection or request count")
			}
			if !enabled {
				if output.Len() != 0 {
					t.Fatal("disabled diagnostics emitted logs")
				}
				return
			}
			for _, private := range []string{
				"Synthetic+/=:% secret",
				url.QueryEscape(
					"Synthetic+/=:% secret",
				),
				"SyntheticOnlyCode",
				base64.StdEncoding.EncodeToString(
					[]byte(
						"knowhub:" +
							url.QueryEscape(
								"Synthetic+/=:% secret",
							),
					),
				),
			} {
				if strings.Contains(output.String(), private) {
					t.Fatal("private value leaked through actual logger")
				}
			}
			phases := map[string]int{}
			decoder := json.NewDecoder(&output)
			for decoder.More() {
				var wrapper struct {
					Message string `json:"msg"`
				}
				if err := decoder.Decode(&wrapper); err != nil {
					t.Fatal("invalid logger JSON")
				}
				var event struct {
					Event string                `json:"event"`
					HTTP  []OIDCProbeHTTPResult `json:"http"`
				}
				if err := json.Unmarshal([]byte(wrapper.Message), &event); err != nil {
					t.Fatal("invalid diagnostic event JSON")
				}
				phases[event.Event]++
				if event.Event == "sourceid_token_http" {
					if len(event.HTTP) != 1 || event.HTTP[0].Transport == nil ||
						event.HTTP[0].Transport.Outcome != "empty_401_basic_challenge" ||
						event.HTTP[0].Transport.SuccessfulWrites != 1 {
						t.Fatal("final logged network/response result incorrect")
					}
				}
			}
			if len(phases) != 3 || phases["sourceid_token_request"] != 1 ||
				phases["sourceid_token_send"] != 1 ||
				phases["sourceid_token_http"] != 1 {
				t.Fatal("expected one event for each diagnostic phase")
			}
		})
	}
}
