package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCProbeChildProcess(t *testing.T) {
	if os.Getenv("OIDC_PROBE_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	flag.CommandLine = flag.NewFlagSet("probe", flag.ContinueOnError)
	os.Args = []string{"probe", "-metadata", os.Getenv("OIDC_PROBE_METADATA"), "-listen", os.Getenv("OIDC_PROBE_LISTEN")}
	os.Exit(run())
}

func TestOIDCProbeProcessClosesListenerAndDoesNotPersistCredentials(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux TLS root and process integration")
	}
	const private = "synthetic-process-private-value"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var nonce, challenge, issuer string
	var fixtureMu sync.Mutex
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{map[string]interface{}{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "probe", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			fixtureMu.Lock()
			expectedNonce, expectedChallenge, expectedIssuer := nonce, challenge, issuer
			fixtureMu.Unlock()
			_ = r.ParseForm()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.URL.RawQuery != "" || r.Form.Get("client_secret") != private || r.Form.Get("code") != private || base64.RawURLEncoding.EncodeToString(sum[:]) != expectedChallenge {
				t.Error("unsafe token exchange")
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": expectedIssuer, "aud": "knowhub", "sub": "synthetic-sub", "nonce": expectedNonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "employee": private})
			token.Header["kid"] = "probe"
			signed, err := token.SignedString(key)
			if err != nil {
				t.Error("signing failed")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id_token": signed, "access_token": private, "refresh_token": private})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer "+private {
				t.Error("wrong UserInfo auth")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": "synthetic-sub", "employee": private})
		default:
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	fixtureMu.Lock()
	issuer = provider.URL
	fixtureMu.Unlock()
	dir := t.TempDir()
	cert := filepath.Join(dir, "public-ca.pem")
	if os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: provider.Certificate().Raw}), 0600) != nil {
		t.Fatal("public CA write")
	}
	metadata := filepath.Join(dir, "metadata.json")
	m := service.OIDCProbeMetadata{Confirmed: true, Issuer: issuer, Authorization: issuer + "/authorize", Token: issuer + "/token", JWKS: issuer + "/jwks", UserInfo: issuer + "/userinfo", RedirectURI: "http://approved-probe.example/probe/callback", ClientID: "knowhub", ClientAuth: "client_secret_post", UserInfoTransport: "bearer_get", Scopes: []string{"openid"}, PKCE: true}
	m.HTTPDiagnostics = true
	data, _ := json.Marshal(m)
	if os.WriteFile(metadata, data, 0600) != nil {
		t.Fatal("metadata write")
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestOIDCProbeChildProcess$")
	child.Env = append(os.Environ(), "OIDC_PROBE_CHILD=1", "OIDC_PROBE_METADATA="+metadata, "OIDC_PROBE_LISTEN="+address, "SSL_CERT_FILE="+cert, "SSRF_WHITELIST=127.0.0.1", "LOG_PATH=", "LLM_DEBUG_LOG=false", "LANGFUSE_ENABLED=false")
	child.Stdin = strings.NewReader(private + "\n")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if child.Start() != nil {
		t.Fatal("child failed to start")
	}
	defer child.Process.Kill()
	decoder := json.NewDecoder(stdout)
	// Bound the integration test independently of the probe's production TTL.
	timedOut := time.AfterFunc(20*time.Second, func() { _ = child.Process.Kill() })
	defer timedOut.Stop()
	var ready map[string]interface{}
	if decoder.Decode(&ready) != nil {
		t.Fatal("probe did not become ready")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + address + "/probe/start")
	if err != nil {
		t.Fatal("start request failed")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	authorization, err := url.Parse(response.Header.Get("Location"))
	if err != nil || response.StatusCode != 302 {
		t.Fatal("authorize missing")
	}
	q := authorization.Query()
	fixtureMu.Lock()
	nonce = q.Get("nonce")
	challenge = q.Get("code_challenge")
	fixtureMu.Unlock()
	callback := url.Values{"state": {q.Get("state")}, "code": {private}}
	response, err = client.Get("http://" + address + "/probe/callback?" + callback.Encode())
	if err != nil {
		t.Fatal("callback request failed")
	}
	var report service.OIDCProbeReport
	if json.NewDecoder(response.Body).Decode(&report) != nil {
		t.Fatal("report decode failed")
	}
	response.Body.Close()
	if response.Header.Get("Set-Cookie") != "" || !report.Validation["completed"] {
		t.Fatal("probe incomplete or created cookie")
	}
	var final service.OIDCProbeReport
	if decoder.Decode(&final) != nil {
		t.Fatal("final report missing")
	}
	if child.Wait() != nil {
		t.Fatal("probe process failed")
	}
	if len(final.HTTP) == 0 || final.HTTP[0].Diagnostics == nil || final.HTTP[0].Diagnostics.Response == nil {
		t.Fatal("process diagnostic output missing")
	}
	encoded, _ := json.Marshal(final)
	if strings.Contains(string(encoded), private) || strings.Contains(stderr.String(), private) {
		t.Fatal("credential leaked")
	}
	if _, err = client.Get("http://" + address + "/probe/start"); err == nil {
		t.Fatal("listener still open after completion")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatal("unexpected persistent files")
	}
	for _, file := range files {
		body, _ := os.ReadFile(filepath.Join(dir, file.Name()))
		if bytes.Contains(body, []byte(private)) {
			t.Fatal("credential written to file")
		}
	}
}
