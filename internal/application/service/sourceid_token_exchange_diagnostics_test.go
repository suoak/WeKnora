package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
)

func TestSourceIDTokenExchangeDiagnosticsReadOnlyIntegration(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Error("invalid form")
				}
				u, p, ok := r.BasicAuth()
				if !ok || u != url.QueryEscape("knowhub") || p != url.QueryEscape("Synthetic+/=:% secret") {
					t.Error("Basic semantics changed")
				}
				if r.Form.Get("code") != "SyntheticOnlyCode" || r.Form.Get("redirect_uri") != "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") != "knowhub" || r.Form.Has("client_secret") || r.Form.Has("code_verifier") {
					t.Error("form semantics changed")
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="synthetic-only"`)
				w.WriteHeader(401)
			}))
			defer server.Close()
			s := &userService{protocolClient: server.Client()}
			cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDDiagnosticsEnable: enabled, SourceIDProtocolOnly: true, ClientID: "knowhub", ClientSecret: "Synthetic+/=:% secret", ClientAuthMethod: "client_secret_basic", TokenEndpoint: server.URL, PKCEEnable: false}
			ctx := SourceIDDiagnosticStatePassed(NewSourceIDDiagnosticContext(context.Background(), enabled))
			result, err := s.exchangeOIDCCode(ctx, cfg, "SyntheticOnlyCode", "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback")
			if result != nil || err == nil || !strings.Contains(err.Error(), "status=401") || calls != 1 {
				t.Fatal("enable/disable changed rejection or request count")
			}
		})
	}
}
