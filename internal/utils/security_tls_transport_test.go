package utils

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const tlsTestHost = "open.feishu.cn"

type tlsTestPKI struct {
	rootCert *x509.Certificate
	rootKey  *rsa.PrivateKey
	rootPool *x509.CertPool
}

func newTLSTestPKI(t *testing.T) *tlsTestPKI {
	t.Helper()
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "WeKnora TLS fallback test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(rootCert)
	return &tlsTestPKI{rootCert: rootCert, rootKey: rootKey, rootPool: pool}
}

func (p *tlsTestPKI) certificate(t *testing.T, dnsName string) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, template, p.rootCert, &key.PublicKey, p.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{leafDER, p.rootCert.Raw}, PrivateKey: key}
}

func tlsTestDeps(ips []string, dial func(context.Context, string, string) (net.Conn, error)) ssrfTransportDependencies {
	return ssrfTransportDependencies{
		lookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			result := make([]net.IPAddr, 0, len(ips))
			for _, ip := range ips {
				result = append(result, net.IPAddr{IP: net.ParseIP(ip)})
			}
			return result, nil
		},
		dialContext:      dial,
		maxConcurrency:   3,
		staggerDelay:     time.Millisecond,
		candidateTimeout: 40 * time.Millisecond,
		overallTimeout:   250 * time.Millisecond,
		logf:             func(string, ...interface{}) {},
	}
}

func tlsFallbackTestConfig() SSRFSafeHTTPClientConfig {
	config := DefaultSSRFSafeHTTPClientConfig()
	config.EnableTLSFallback = true
	return config
}

func successfulTLSPipe(t *testing.T, cert tls.Certificate, serverName chan<- string) (net.Conn, <-chan struct{}) {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		serverTLS := tls.Server(server, &tls.Config{
			Certificates: []tls.Certificate{cert},
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				if serverName != nil {
					serverName <- hello.ServerName
				}
				return nil, nil
			},
		})
		if err := serverTLS.Handshake(); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, serverTLS)
	}()
	return client, done
}

func blackholeTLSPipe() (net.Conn, <-chan struct{}) {
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		_, _ = io.Copy(io.Discard, server)
	}()
	return client, done
}

func dialTestTLS(
	t *testing.T,
	deps ssrfTransportDependencies,
	config *tls.Config,
) (net.Conn, error) {
	t.Helper()
	return ssrfSafeDialTLSContextWithDeps(
		context.Background(), "tcp", net.JoinHostPort(tlsTestHost, "443"),
		func() *tls.Config { return config }, deps,
	)
}

func TestSSRFTLSFallbackPolicyConstants(t *testing.T) {
	if ssrfTLSMaxConcurrency != 3 {
		t.Fatalf("max concurrency=%d, want 3", ssrfTLSMaxConcurrency)
	}
	if ssrfTLSStaggerDelay != 200*time.Millisecond {
		t.Fatalf("stagger delay=%s, want 200ms", ssrfTLSStaggerDelay)
	}
	if ssrfTLSCandidateTimeout != 5*time.Second {
		t.Fatalf("candidate timeout=%s, want 5s", ssrfTLSCandidateTimeout)
	}
	if ssrfTLSOverallDialTimeout != 10*time.Second {
		t.Fatalf("overall timeout=%s, want 10s", ssrfTLSOverallDialTimeout)
	}
}

func TestSSRFSafeDialTLSBoundsConcurrency(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	deps := tlsTestDeps(
		[]string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "8.8.4.4", "1.0.0.1"},
		func(ctx context.Context, _, _ string) (net.Conn, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	)
	deps.maxConcurrency = 3
	deps.staggerDelay = time.Millisecond
	deps.candidateTimeout = time.Second
	deps.overallTimeout = 30 * time.Millisecond
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
	if maximum.Load() != 3 {
		t.Fatalf("maximum concurrent candidates=%d, want 3", maximum.Load())
	}
}

func TestSSRFSafeDialTLSFirstTCPFailureFallsBack(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, tlsTestHost)
	var calls atomic.Int32
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(context.Context, string, string) (net.Conn, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("connection refused")
		}
		conn, _ := successfulTLSPipe(t, cert, nil)
		return conn, nil
	})
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool})
	if err != nil {
		t.Fatal(err)
	}
	tlsConn, ok := conn.(*tls.Conn)
	if !ok || !tlsConn.ConnectionState().HandshakeComplete {
		t.Fatalf("DialTLSContext returned before TLS handshake completion: %T", conn)
	}
	_ = conn.Close()
	if calls.Load() != 2 {
		t.Fatalf("dial calls=%d, want 2", calls.Load())
	}
}

func TestSSRFSafeDialTLSTLSBlackholeFallsBackAndClosesLoser(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, tlsTestHost)
	var calls atomic.Int32
	var blackholeClosed <-chan struct{}
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(context.Context, string, string) (net.Conn, error) {
		if calls.Add(1) == 1 {
			conn, closed := blackholeTLSPipe()
			blackholeClosed = closed
			return conn, nil
		}
		conn, _ := successfulTLSPipe(t, cert, nil)
		return conn, nil
	})
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case <-blackholeClosed:
	case <-time.After(time.Second):
		t.Fatal("TLS black-hole loser socket was not closed")
	}
}

func TestSSRFSafeDialTLSCertificateFailureFallsBack(t *testing.T) {
	pki := newTLSTestPKI(t)
	wrongCert := pki.certificate(t, "wrong.example")
	validCert := pki.certificate(t, tlsTestHost)
	var calls atomic.Int32
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(context.Context, string, string) (net.Conn, error) {
		cert := wrongCert
		if calls.Add(1) == 2 {
			cert = validCert
		}
		conn, _ := successfulTLSPipe(t, cert, nil)
		return conn, nil
	})
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestSSRFSafeDialTLSSeveralFailuresThenSuccess(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, tlsTestHost)
	var calls atomic.Int32
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "8.8.4.4"}, func(context.Context, string, string) (net.Conn, error) {
		if calls.Add(1) < 4 {
			return nil, errors.New("edge unavailable")
		}
		conn, _ := successfulTLSPipe(t, cert, nil)
		return conn, nil
	})
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestSSRFSafeDialTLSAllTCPFailures(t *testing.T) {
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if err == nil || !strings.Contains(err.Error(), "attempted=2 total=2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSSRFSafeDialTLSDiagnosticsDoNotLogErrorSecrets(t *testing.T) {
	const secret = "app_secret=do-not-log tenant_access_token=do-not-log Authorization=Bearer-do-not-log"
	var logs strings.Builder
	deps := tlsTestDeps([]string{"8.8.8.8"}, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New(secret)
	})
	deps.logf = func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(&logs, format, args...)
	}
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if err == nil {
		t.Fatal("expected dial failure")
	}
	for _, forbidden := range []string{"app_secret", "tenant_access_token", "Authorization", "Bearer-do-not-log"} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("diagnostic logs leaked %q: %s", forbidden, logs.String())
		}
	}
}

func TestSSRFSafeDialTLSAllTLSFailures(t *testing.T) {
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(context.Context, string, string) (net.Conn, error) {
		conn, _ := blackholeTLSPipe()
		return conn, nil
	})
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if err == nil || !strings.Contains(err.Error(), "attempted=2 total=2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSSRFSafeDialTLSCallerCancellationStopsAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1", "9.9.9.9"}, func(ctx context.Context, _, _ string) (net.Conn, error) {
		calls.Add(1)
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	})
	_, err := ssrfSafeDialTLSContextWithDeps(ctx, "tcp", tlsTestHost+":443", func() *tls.Config {
		return &tls.Config{}
	}, deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("dial calls=%d, want 1", calls.Load())
	}
}

func TestSSRFSafeDialTLSOverallDeadlineIsHardBudget(t *testing.T) {
	ips := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "8.8.4.4", "1.0.0.1", "208.67.222.222"}
	deps := tlsTestDeps(ips, func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	deps.staggerDelay = 20 * time.Millisecond
	deps.candidateTimeout = time.Second
	deps.overallTimeout = 35 * time.Millisecond
	started := time.Now()
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("overall deadline was not a hard budget: %s", elapsed)
	}
	if err == nil || strings.Contains(err.Error(), fmt.Sprintf("attempted=%d", len(ips))) {
		t.Fatalf("expected budget to stop before all candidates, got %v", err)
	}
}

func TestSSRFSafeDialTLSOverallDeadlineIncludesDNS(t *testing.T) {
	deps := tlsTestDeps(nil, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("must not dial")
	})
	deps.overallTimeout = 20 * time.Millisecond
	deps.lookupIPAddr = func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	started := time.Now()
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "attempted=0 total=0") {
		t.Fatalf("unexpected DNS budget error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("DNS exceeded overall budget: %s", elapsed)
	}
}

func TestSSRFSafeDialTLSWinnerCancelsLosersWithoutLeak(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, tlsTestHost)
	var calls atomic.Int32
	var active atomic.Int32
	loserClosed := make(chan struct{}, 2)
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1", "9.9.9.9"}, func(context.Context, string, string) (net.Conn, error) {
		call := calls.Add(1)
		active.Add(1)
		if call == 2 {
			conn, done := successfulTLSPipe(t, cert, nil)
			go func() { <-done; active.Add(-1) }()
			return conn, nil
		}
		conn, done := blackholeTLSPipe()
		go func() {
			<-done
			active.Add(-1)
			loserClosed <- struct{}{}
		}()
		return conn, nil
	})
	before := runtime.NumGoroutine()
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case <-loserClosed:
	case <-time.After(time.Second):
		t.Fatal("loser did not observe socket close")
	}
	deadline := time.Now().Add(time.Second)
	for active.Load() != 0 && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if active.Load() != 0 {
		t.Fatalf("active TLS test workers=%d", active.Load())
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

func TestSSRFSafeDialTLSValidatesAllBeforeAnyDialAndNoReresolve(t *testing.T) {
	var lookups atomic.Int32
	var dials atomic.Int32
	deps := tlsTestDeps(nil, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	})
	deps.lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		lookups.Add(1)
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, err := dialTestTLS(t, deps, &tls.Config{})
	if err == nil || !strings.Contains(err.Error(), "restricted IP") {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookups.Load() != 1 || dials.Load() != 0 {
		t.Fatalf("lookups=%d dials=%d, want 1/0", lookups.Load(), dials.Load())
	}
}

func TestSSRFSafeDialTLSServerNameAndVerification(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, tlsTestHost)
	serverName := make(chan string, 1)
	deps := tlsTestDeps([]string{"8.8.8.8"}, func(context.Context, string, string) (net.Conn, error) {
		conn, _ := successfulTLSPipe(t, cert, serverName)
		return conn, nil
	})
	conn, err := dialTestTLS(t, deps, &tls.Config{RootCAs: pki.rootPool, ServerName: "must-be-overridden.example"})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got := <-serverName; got != tlsTestHost {
		t.Fatalf("TLS ServerName=%q, want %q", got, tlsTestHost)
	}

	var dials atomic.Int32
	deps.dialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not dial with verification disabled")
	}
	_, err = dialTestTLS(t, deps, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // rejection test
	if err == nil || !strings.Contains(err.Error(), "verification must remain enabled") || dials.Load() != 0 {
		t.Fatalf("verification guard error=%v dials=%d", err, dials.Load())
	}
}

func TestSSRFSafeTransportHTTPRemainsPinnedAndNonTLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	serverAddr := server.Listener.Addr().String()
	var dialedAddress string
	deps := tlsTestDeps([]string{"8.8.8.8"}, func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialedAddress = addr
		return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
	})
	transport := newSSRFSafeTransportWithDependencies(tlsFallbackTestConfig(), deps)
	client := &http.Client{Transport: transport}
	_, serverPort, err := net.SplitHostPort(serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://" + net.JoinHostPort("example.com", serverPort))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !strings.HasPrefix(dialedAddress, "8.8.8.8:") {
		t.Fatalf("HTTP dial was not pinned: %s", dialedAddress)
	}
}

func TestSSRFSafeTransportNegotiatesHTTP2WithCurrentTLSConfig(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, "example.com")
	proto := make(chan int, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto <- r.ProtoMajor
		_, _ = io.WriteString(w, "ok")
	}))
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	serverAddr := server.Listener.Addr().String()
	deps := tlsTestDeps([]string{"8.8.8.8"}, func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
	})
	transport := newSSRFSafeTransportWithDependencies(tlsFallbackTestConfig(), deps)
	transport.TLSClientConfig.RootCAs = pki.rootPool
	// A custom DialTLSContext must enforce its own timeout. net/http documents
	// TLSHandshakeTimeout as ignored on this path; a 1ns value must not affect
	// the already-completed handshake returned by our dialer.
	transport.TLSHandshakeTimeout = time.Nanosecond
	client := &http.Client{Transport: transport}
	_, serverPort, err := net.SplitHostPort(serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("https://" + net.JoinHostPort("example.com", serverPort))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := <-proto; got != 2 {
		t.Fatalf("negotiated HTTP/%d, want HTTP/2", got)
	}
}

func TestSSRFSafeTransportProxiedHTTPSDoesNotUseDialTLSContext(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method=%s, want CONNECT", r.Method)
		}
		http.Error(w, "proxy intentionally stopped CONNECT", http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}

	transport := NewSSRFSafeTransport(tlsFallbackTestConfig())
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.DialContext = (&net.Dialer{}).DialContext
	var tlsDials atomic.Int32
	originalDialTLS := transport.DialTLSContext
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		tlsDials.Add(1)
		return originalDialTLS(ctx, network, addr)
	}
	client := &http.Client{Transport: transport}
	_, _ = client.Get("https://example.com/")
	if tlsDials.Load() != 0 {
		t.Fatalf("DialTLSContext calls=%d, want 0", tlsDials.Load())
	}
}

func TestSSRFSafeTransportTLSFallbackHappensBeforePOSTBodyWrite(t *testing.T) {
	pki := newTLSTestPKI(t)
	cert := pki.certificate(t, "example.com")
	var handlerCalls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != "credential-body-sentinel" {
			t.Errorf("body=%q", body)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	serverAddr := server.Listener.Addr().String()
	var dialCalls atomic.Int32
	var loserClosed <-chan struct{}
	deps := tlsTestDeps([]string{"8.8.8.8", "1.1.1.1"}, func(ctx context.Context, network, _ string) (net.Conn, error) {
		if dialCalls.Add(1) == 1 {
			conn, closed := blackholeTLSPipe()
			loserClosed = closed
			return conn, nil
		}
		return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
	})
	transport := newSSRFSafeTransportWithDependencies(tlsFallbackTestConfig(), deps)
	transport.TLSClientConfig.RootCAs = pki.rootPool
	_, serverPort, err := net.SplitHostPort(serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(
		http.MethodPost,
		"https://"+net.JoinHostPort("example.com", serverPort)+"/token",
		strings.NewReader("credential-body-sentinel"),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if handlerCalls.Load() != 1 {
		t.Fatalf("POST handler calls=%d, want exactly 1", handlerCalls.Load())
	}
	select {
	case <-loserClosed:
	case <-time.After(time.Second):
		t.Fatal("TLS loser was not closed")
	}
}

type redirectRecordingTransport struct {
	mu    sync.Mutex
	calls []string
}

func (r *redirectRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req.URL.String())
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{"https://127.0.0.1/private"}},
		Body:       io.NopCloser(strings.NewReader("redirect")),
		Request:    req,
	}, nil
}

func TestHTTPSRedirectToRestrictedIPIsRejectedBeforeSecondRoundTrip(t *testing.T) {
	base := &redirectRecordingTransport{}
	client := &http.Client{
		Transport:     base,
		CheckRedirect: newSSRFCheckRedirect(10),
	}
	_, err := client.Get("https://example.com/start")
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("unexpected redirect error: %v", err)
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.calls) != 1 {
		t.Fatalf("round trips=%v, restricted redirect must cause zero follow-up dial", base.calls)
	}
}
