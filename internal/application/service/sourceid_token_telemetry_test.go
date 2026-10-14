package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSourceIDTokenTelemetryPreservesRequestAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"empty_basic", "", "", 401},
		{"oauth", `{"error":"invalid_client","error_description":"SYNTHETIC_PRIVATE_CODE SYNTHETIC_PRIVATE_SECRET"}`, "application/json", 401},
		{"gateway", `<html>SYNTHETIC_PRIVATE_CODE SYNTHETIC_PRIVATE_SECRET</html>`, "text/html", 401},
		{"success", `{"access_token":"SYNTHETIC_ACCESS","id_token":"SYNTHETIC_ID","refresh_token":"SYNTHETIC_REFRESH"}`, "application/json", 200},
		{"malformed_success", `{SYNTHETIC_PRIVATE_SECRET`, "application/json", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" {
					t.Error("method changed")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				user, password, ok := r.BasicAuth()
				if !ok || user != "knowhub" || password != "SYNTHETIC_PRIVATE_SECRET" {
					t.Error("Basic changed")
				}
				if r.Form.Get("code") != "SYNTHETIC_PRIVATE_CODE" || r.Form.Has("code_verifier") || r.Form.Has("client_secret") {
					t.Error("form changed")
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="SYNTHETIC_PRIVATE_SECRET"`)
				w.Header().Set("Server", "nginx/SYNTHETIC_PRIVATE_SECRET")
				w.Header().Set("Via", "1.1 SYNTHETIC_PRIVATE_SECRET")
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := server.Client() // trusts only this synthetic server certificate
			form := url.Values{"grant_type": {"authorization_code"}, "code": {"SYNTHETIC_PRIVATE_CODE"}, "client_id": {"knowhub"}, "redirect_uri": {"http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"}}
			req, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, strings.NewReader(form.Encode()))
			req.SetBasicAuth(url.QueryEscape("knowhub"), url.QueryEscape("SYNTHETIC_PRIVATE_SECRET"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			originalAuth := req.Header.Get("Authorization")
			// Existing caller hooks must still run once; observation composes
			// httptrace instead of replacing it.
			var callerWrites atomic.Int64
			req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { callerWrites.Add(1) }}))
			req, telemetry := observeSourceIDTokenRequest(req, "synthetic-trace", true, false, true, true)
			report := &OIDCProbeReport{}
			client.Transport = &probeDiagnosticTransport{base: client.Transport, report: report, endpoints: map[string]string{"token": server.URL, "callback": form.Get("redirect_uri")}, clientID: "knowhub", diagnostics: true}
			resp, err := client.Do(req)
			telemetry.response(resp, err)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			telemetry.complete()
			if string(body) != tc.body || resp.StatusCode != tc.status || calls.Load() != 1 || callerWrites.Load() != 1 || req.Header.Get("Authorization") != originalAuth {
				t.Fatal("diagnostics changed response/request count")
			}
			if telemetry.SuccessfulWrites != 1 || telemetry.Retry || telemetry.Remote == "NOT_CAPTURED" || telemetry.TLS == "NOT_CAPTURED" {
				t.Fatal("missing network observation")
			}
			if !telemetry.SecretLoaded || !telemetry.PKCEDisabled || telemetry.State != "PASS" || telemetry.Nonce != "NOT_YET_EXECUTED" {
				t.Fatal("construction metadata incorrect")
			}
			if report.HTTP[0].Diagnostics.Response.BytesRead != int64(len(tc.body)) || !report.HTTP[0].Diagnostics.Response.BodyEOF {
				t.Fatal("body accounting changed")
			}
			raw, _ := json.Marshal(struct {
				Network *sourceIDTokenTelemetry
				Report  *OIDCProbeReport
			}{telemetry, report})
			for _, canary := range []string{"SYNTHETIC_PRIVATE_CODE", "SYNTHETIC_PRIVATE_SECRET", "SYNTHETIC_ACCESS", "SYNTHETIC_ID", "SYNTHETIC_REFRESH", originalAuth, base64.StdEncoding.EncodeToString([]byte("knowhub:SYNTHETIC_PRIVATE_SECRET"))} {
				if strings.Contains(string(raw), canary) {
					t.Fatalf("private value leaked for case %s", tc.name)
				}
			}
		})
	}
}

func TestSourceIDTokenTelemetryDuplicateFieldsAndBoundedConcurrentTrace(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://sid.ruijie.com.cn/oidc/accessToken", strings.NewReader("code=synthetic-a&code=synthetic-b&client_id=knowhub&grant_type=authorization_code&secret_unknown=synthetic-private"))
	req.Header["Authorization"] = []string{"Basic synthetic-a", "Basic synthetic-b"}
	req.Header["authorization"] = []string{"Basic synthetic-c"}
	_, d := observeSourceIDTokenRequest(req, "synthetic-trace", true, false, false, true)
	if d.AuthorizationCount != 3 || !d.DuplicateAuthorization || !d.DuplicateBodyFields || d.KnownBodyCounts["code"] != 2 || d.BodyValueCount != 5 || d.State != "NOT_CAPTURED" {
		t.Fatal("duplicate accounting incorrect")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d.event("dns_start", "started"); json.Marshal(d) }()
	}
	wg.Wait()
	if len(d.Events) != 64 || d.Dropped != 36 {
		t.Fatal("event bound failed")
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "synthetic-private") || strings.Contains(string(raw), "secret_unknown") {
		t.Fatal("unknown fields leaked")
	}
}

func TestSourceIDTokenTelemetryNetworkErrorsAndRedirects(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://sid.ruijie.com.cn/oidc/accessToken", nil)
	r, d := observeSourceIDTokenRequest(req, "synthetic", false, false, false, true)
	hooks := httptrace.ContextClientTrace(r.Context())
	hooks.TLSHandshakeDone(tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "h2"}, nil)
	hooks.WroteRequest(httptrace.WroteRequestInfo{})
	hooks.WroteRequest(httptrace.WroteRequestInfo{})
	d.response(&http.Response{StatusCode: 302, Proto: "HTTP/2.0", Header: http.Header{"Location": {"https://untrusted.invalid/?code=synthetic-private"}}}, nil)
	if !d.Retry || d.WriteAttempts != 2 || !d.Redirect || d.HTTP != "HTTP/2.0" || d.TLS != "TLS1.3" {
		t.Fatal("trace accounting incorrect")
	}
	d.response(nil, &tls.CertificateVerificationError{})
	if d.Failure != "tls_error" {
		t.Fatal("TLS failure classification missing")
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "untrusted.invalid") || strings.Contains(string(raw), "synthetic-private") {
		t.Fatal("redirect leaked")
	}
}
