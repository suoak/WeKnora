package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
)

func TestOIDCProbeHTTPDiagnostics(t *testing.T) {
	for _, kind := range []string{"oauth", "www", "vendor", "html", "private_description", "invalid_200", "unknown_vendor", "private_correlation", "token_correlation", "large", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			withOIDCSSRFWhitelist(t, "127.0.0.1")
			const secret = "synthetic private secret &+:/"
			const code = "synthetic private code &+:/"
			const sensitive = "synthetic-private-person@example.com"
			const secretID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			clientSecret := secret
			if kind == "private_correlation" {
				clientSecret = secretID
			}
			var calls int
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Error("token protocol changed")
				}
				_ = r.ParseForm()
				id, pw, ok := r.BasicAuth()
				if !ok || id != url.QueryEscape("probe id:é") || pw != url.QueryEscape(clientSecret) {
					t.Error("OAuth Basic encoding incorrect")
				}
				if _, exists := r.PostForm["client_secret"]; exists {
					t.Error("Basic secret entered body")
				}
				if _, exists := r.PostForm["code_verifier"]; exists {
					t.Error("disabled PKCE entered body")
				}
				if r.PostForm.Get("code") != code || r.PostForm.Get("redirect_uri") != approvedSourceIDProbeCallbackURI {
					t.Error("code or callback changed")
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Request-ID", "0123456789abcdef0123456789abcdef")
				w.Header().Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
				w.Header().Set("Set-Cookie", sensitive)
				if kind == "private_correlation" {
					w.Header().Set("X-Request-ID", clientSecret)
				}
				if kind == "token_correlation" {
					w.Header().Set("X-Request-ID", secretID)
				}
				if kind == "www" {
					w.Header().Set("WWW-Authenticate", `Basic realm="`+clientSecret+`", error="invalid_client", error_description="`+sensitive+`"`)
				}
				status := 401
				if kind == "invalid_200" {
					status = 200
				}
				if kind == "html" {
					w.Header().Set("Content-Type", "text/html")
				}
				w.WriteHeader(status)
				switch kind {
				case "vendor":
					io.WriteString(w, `{"errorCode":"SID_INVALID_CLIENT","message":"`+sensitive+`"}`)
				case "unknown_vendor":
					io.WriteString(w, `{"errorCode":"EMPLOYEE_123456","message":"`+sensitive+`"}`)
				case "html":
					io.WriteString(w, "<!doctype html><html>"+sensitive+"</html>")
				case "private_description":
					json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": clientSecret + code + sensitive})
				case "token_correlation":
					json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "access_token": secretID})
				case "invalid_200":
					io.WriteString(w, "not JSON "+sensitive)
				case "large":
					io.WriteString(w, strings.Repeat(sensitive, 512))
				default:
					io.WriteString(w, `{"error":"invalid_client"}`)
				}
			}))
			defer provider.Close()
			report := OIDCProbeReport{}
			transport := &probeDiagnosticTransport{base: provider.Client().Transport, endpoints: map[string]string{"token": provider.URL, "callback": approvedSourceIDProbeCallbackURI}, report: &report, callbackReceived: time.Now(), clientAuth: "client_secret_basic", diagnostics: kind != "disabled", clientID: "probe id:é", publicErrorCodes: []string{"SID_INVALID_CLIENT"}}
			svc := &userService{protocolClient: &http.Client{Transport: transport}}
			cfg := config.OIDCAuthConfig{TokenEndpoint: provider.URL, ClientID: "probe id:é", ClientSecret: clientSecret, ClientAuthMethod: "client_secret_basic"}
			_, err := svc.exchangeOIDCCode(context.Background(), &cfg, code, approvedSourceIDProbeCallbackURI)
			if err == nil || calls != 1 || len(report.HTTP) != 1 {
				t.Fatal("unexpected success or retry")
			}
			d := report.HTTP[0]
			if kind == "disabled" {
				if d.Diagnostics != nil {
					t.Fatal("opt-in diagnostics enabled by default")
				}
				return
			}
			if d.Diagnostics == nil || d.Diagnostics.Response == nil {
				t.Fatal("diagnostic evidence missing")
			}
			request, response := d.Diagnostics.Request, d.Diagnostics.Response
			if request.Method != "POST" || request.Host == "" || request.Path != "" || !request.HostMatchesEndpoint || request.ContentType != "application/x-www-form-urlencoded" || request.AuthorizationType != "Basic" || !request.BasicFormatValid || !request.FormInspected || request.FieldPresence["client_secret"] || request.FieldPresence["code_verifier"] || !request.FieldPresence["code"] || !request.FieldChecks["redirect_uri_matches_config"] || !request.FieldChecks["client_id_matches_config"] || request.SentUTC == "" {
				t.Fatal("safe request structure incorrect")
			}
			if response.BytesRead <= 0 || response.BodyElapsedMS < 0 {
				t.Fatal("response observation missing")
			}
			switch kind {
			case "oauth":
				if response.OAuthError != "invalid_client" || response.BodyKind != "json_object" {
					t.Fatal("OAuth error misclassified")
				}
			case "www":
				if !response.WWWPresent || response.WWWError != "invalid_client" || !response.WWWRealmPresent || !response.WWWDescriptionPresent {
					t.Fatal("WWW challenge lost")
				}
			case "vendor":
				if response.VendorCode != "SID_INVALID_CLIENT" {
					t.Fatal("approved public vendor code lost")
				}
			case "unknown_vendor":
				if response.VendorCode != "" || !response.VendorCodeSuppressed {
					t.Fatal("unapproved vendor code leaked")
				}
			case "html":
				if response.BodyKind != "html" || !response.GatewaySuspected {
					t.Fatal("HTML error misclassified")
				}
			case "private_description":
				if !response.ErrorDescriptionPresent || response.ErrorDescriptionSummary != "withheld_untrusted_description" {
					t.Fatal("description policy bypassed")
				}
			case "invalid_200":
				if d.Status != 200 || response.BodyKind != "non_json_or_incomplete" {
					t.Fatal("invalid success response misclassified")
				}
			case "private_correlation", "token_correlation":
				if response.Correlation["X-Request-ID"].Fingerprint != "" {
					t.Fatal("credential correlation fingerprint leaked")
				}
			case "large":
				if !response.PrefixTruncated || response.BodyEOF {
					t.Fatal("partial response claimed complete")
				}
			}
			encoded, _ := json.Marshal(report)
			for _, private := range []string{secret, code, sensitive, secretID, "EMPLOYEE_123456", "0123456789abcdef0123456789abcdef"} {
				if strings.Contains(string(encoded), private) {
					t.Fatal("sensitive response/request escaped diagnostic filter")
				}
			}
		})
	}
}

func TestOIDCProbeDiagnosticPolicy(t *testing.T) {
	withOIDCSSRFWhitelist(t, "sid.example")
	m := OIDCProbeMetadata{Confirmed: true, Issuer: "https://sid.example", Authorization: "https://sid.example/authorize", Token: "https://sid.example/token", JWKS: "https://sid.example/jwks", UserInfo: "https://sid.example/userinfo", RedirectURI: approvedSourceIDProbeCallbackURI, ClientID: "knowhub", ClientAuth: "client_secret_basic", UserInfoTransport: "bearer_get", Scopes: []string{"openid"}, HTTPDiagnostics: true, PublicErrorCodes: []string{"SID_INVALID_CLIENT"}}
	if err := ValidateOIDCProbeMetadata(m); err != nil {
		t.Fatal("public diagnostic policy rejected")
	}
	for _, code := range []string{"employee@example.com", "SECRET with spaces", "https://example.com/token", "eyJ.mock.jwt", strings.Repeat("A", 49)} {
		m.PublicErrorCodes = []string{code}
		if ValidateOIDCProbeMetadata(m) == nil {
			t.Fatal("unsafe public code policy accepted")
		}
	}
}
