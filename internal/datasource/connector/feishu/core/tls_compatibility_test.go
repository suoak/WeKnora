package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestFeishuTLSCompatibilityKeepsVerifiedHandshakeAndRedirectBoundary(t *testing.T) {
	var hellos, requests, followed atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "/must-not-follow", http.StatusTemporaryRedirect)
		if r.URL.Path == "/must-not-follow" {
			followed.Add(1)
		}
	}))
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		hellos.Add(1)
		for _, curve := range hello.SupportedCurves {
			if curve != tls.X25519 && curve != tls.CurveP256 && curve != tls.CurveP384 {
				t.Errorf("unexpected Feishu ClientHello group %d", curve)
			}
		}
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	client := NewOAuthHTTPClient()
	safety := client.Transport.(feishusafety.Transport)
	guard := safety.Base.(*utils.SSRFValidatingRoundTripper)
	transport := guard.Base.(*http.Transport)
	require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	// Route only this isolated test to loopback; retain the real TLS config,
	// trust verification and no-redirect policy on the wire.
	client.Transport = transport
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := tls.Dialer{Config: transport.TLSClientConfig.Clone()}
		return dialer.DialContext(ctx, network, addr)
	}
	_, err := client.Post(server.URL, "application/json", nil)
	var invalid *tls.CertificateVerificationError
	require.ErrorAs(t, err, &invalid)
	require.Zero(t, requests.Load(), "untrusted TLS must send no OAuth request")
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig.RootCAs = roots
	response, err := client.Post(server.URL, "application/json", nil)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, response.Body)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	require.EqualValues(t, 1, requests.Load())
	require.Zero(t, followed.Load())
	require.GreaterOrEqual(t, hellos.Load(), int32(2))
}
