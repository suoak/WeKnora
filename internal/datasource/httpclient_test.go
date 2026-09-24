package datasource

import (
	"net/http"
	"testing"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestNewConnectorHTTPClientWithTLSFallbackKeepsClientBudget(t *testing.T) {
	client := NewConnectorHTTPClientWithTLSFallback(30 * time.Second)
	if client.Timeout != 30*time.Second {
		t.Fatalf("client timeout=%s, want 30s", client.Timeout)
	}
	guard, ok := client.Transport.(*secutils.SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("transport=%T, want SSRF validating wrapper", client.Transport)
	}
	transport, ok := guard.Base.(*http.Transport)
	if !ok {
		t.Fatalf("base transport=%T, want *http.Transport", guard.Base)
	}
	if transport.DialContext == nil || transport.DialTLSContext == nil {
		t.Fatal("connector transport must provide pinned HTTP and TLS-aware HTTPS dialers")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("connector transport must keep TLS certificate verification enabled")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("connector transport must preserve HTTP/2 negotiation with custom dialers")
	}
}

func TestNewConnectorHTTPClientPreservesDefaultTLSPath(t *testing.T) {
	client := NewConnectorHTTPClient(30 * time.Second)
	guard := client.Transport.(*secutils.SSRFValidatingRoundTripper)
	transport := guard.Base.(*http.Transport)
	if transport.DialTLSContext != nil {
		t.Fatal("default connector client must not opt unrelated connectors into TLS fallback")
	}
}

func TestValidateConnectorBaseURLBlocksLoopback(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	err := ValidateConnectorBaseURL("http://127.0.0.1:8000")
	if err == nil {
		t.Fatal("expected loopback base_url to be rejected")
	}
}

func TestValidateConnectorBaseURLAllowsPublicHTTPS(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	if err := ValidateConnectorBaseURL("https://open.feishu.cn"); err != nil {
		t.Fatalf("expected public base_url to pass: %v", err)
	}
}
