package utils

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type recordingRoundTripper struct {
	called bool
}

func (r *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return nil, fmt.Errorf("unexpected network call")
}

func TestSSRFSafeClientValidatesInitialRequestAtFinalSink(t *testing.T) {
	base := &recordingRoundTripper{}
	client := NewSSRFSafeHTTPClientWithTransport(DefaultSSRFSafeHTTPClientConfig(), base)
	req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("expected final-sink SSRF rejection, got %v", err)
	}
	if base.called {
		t.Fatal("unsafe request reached the base transport")
	}
}

func TestSSRFSafeDialContextRejectsRestrictedPortAtFinalSink(t *testing.T) {
	_, err := SSRFSafeDialContext(context.Background(), "tcp", "example.com:6379")
	if err == nil || !strings.Contains(err.Error(), "port 6379") {
		t.Fatalf("expected restricted-port error, got %v", err)
	}
}

func TestSSRFSafeDialContextFallsBackAfterPerIPTimeout(t *testing.T) {
	if ssrfDialAttemptTimeout != 5*time.Second {
		t.Fatalf("per-IP dial budget = %v, want 5s", ssrfDialAttemptTimeout)
	}

	firstIP := "8.8.8.8"
	secondIP := "1.1.1.1"
	var attempted []string
	peerClosed := make(chan struct{})

	conn, err := ssrfSafeDialContextWithDeps(
		context.Background(),
		"tcp",
		"open.feishu.cn:443",
		ssrfDialDependencies{
			lookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP(firstIP)}, {IP: net.ParseIP(secondIP)}}, nil
			},
			dialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				attempted = append(attempted, addr)
				if strings.HasPrefix(addr, firstIP) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				client, peer := net.Pipe()
				go func() {
					_ = peer.Close()
					close(peerClosed)
				}()
				return client, nil
			},
			perIPTimeout: 10 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("expected second IP to connect, got %v", err)
	}
	defer conn.Close()
	<-peerClosed

	want := []string{net.JoinHostPort(firstIP, "443"), net.JoinHostPort(secondIP, "443")}
	if fmt.Sprint(attempted) != fmt.Sprint(want) {
		t.Fatalf("dial attempts = %v, want %v", attempted, want)
	}
}

func TestSSRFSafeDialContextValidatesAllIPsBeforeDialing(t *testing.T) {
	dialCalls := 0
	_, err := ssrfSafeDialContextWithDeps(
		context.Background(),
		"tcp",
		"open.feishu.cn:443",
		ssrfDialDependencies{
			lookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{
					{IP: net.ParseIP("8.8.8.8")},
					{IP: net.ParseIP("127.0.0.1")},
				}, nil
			},
			dialContext: func(context.Context, string, string) (net.Conn, error) {
				dialCalls++
				return nil, fmt.Errorf("must not dial")
			},
			perIPTimeout: time.Millisecond,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "restricted IP") {
		t.Fatalf("expected restricted-IP rejection, got %v", err)
	}
	if dialCalls != 0 {
		t.Fatalf("dial called %d times before every IP was validated", dialCalls)
	}
}

func TestSSRFSafeDialContextReturnsLastErrorWhenAllIPsFail(t *testing.T) {
	lastErr := errors.New("second address refused connection")
	var attempted []string

	_, err := ssrfSafeDialContextWithDeps(
		context.Background(),
		"tcp",
		"open.feishu.cn:443",
		ssrfDialDependencies{
			lookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{
					{IP: net.ParseIP("8.8.8.8")},
					{IP: net.ParseIP("1.1.1.1")},
				}, nil
			},
			dialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
				attempted = append(attempted, addr)
				if len(attempted) == 2 {
					return nil, lastErr
				}
				return nil, errors.New("first address timed out")
			},
			perIPTimeout: time.Millisecond,
		},
	)
	if !errors.Is(err, lastErr) {
		t.Fatalf("error = %v, want wrapped last dial error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "failed to connect to validated addresses for open.feishu.cn") {
		t.Fatalf("error lacks safe connection context: %v", err)
	}
	if len(attempted) != 2 {
		t.Fatalf("dial attempts = %d, want 2", len(attempted))
	}
}

func TestSSRFSafeDialContextStopsFallbackWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialCalls := 0
	started := time.Now()

	_, err := ssrfSafeDialContextWithDeps(
		ctx,
		"tcp",
		"open.feishu.cn:443",
		ssrfDialDependencies{
			lookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{
					{IP: net.ParseIP("8.8.8.8")},
					{IP: net.ParseIP("1.1.1.1")},
				}, nil
			},
			dialContext: func(attemptCtx context.Context, _, _ string) (net.Conn, error) {
				dialCalls++
				cancel()
				<-attemptCtx.Done()
				return nil, attemptCtx.Err()
			},
			perIPTimeout: time.Second,
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if dialCalls != 1 {
		t.Fatalf("dial calls after cancellation = %d, want 1", dialCalls)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("canceled dial returned too slowly: %v", elapsed)
	}
}

// TestNewSSRFSafeTransport_SharedAcrossClients verifies that a single transport
// can back multiple clients (global connection pooling) while each client keeps
// its own timeout and a redirect policy.
func TestNewSSRFSafeTransport_SharedAcrossClients(t *testing.T) {
	shared := NewSSRFSafeTransport(DefaultSSRFSafeHTTPClientConfig())

	cfg := DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = 15 * time.Second
	first := NewSSRFSafeHTTPClientWithTransport(cfg, shared)

	cfg.Timeout = 45 * time.Second
	second := NewSSRFSafeHTTPClientWithTransport(cfg, shared)

	if first == second {
		t.Fatal("expected distinct HTTP clients")
	}
	firstGuard, ok := first.Transport.(*SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating wrapper, got %T", first.Transport)
	}
	secondGuard, ok := second.Transport.(*SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating wrapper, got %T", second.Transport)
	}
	if firstGuard.Base != secondGuard.Base || firstGuard.Base != http.RoundTripper(shared) {
		t.Fatal("expected clients to share the supplied base transport")
	}
	if first.Timeout != 15*time.Second {
		t.Fatalf("unexpected first timeout: got %v, want %v", first.Timeout, 15*time.Second)
	}
	if second.Timeout != 45*time.Second {
		t.Fatalf("unexpected second timeout: got %v, want %v", second.Timeout, 45*time.Second)
	}
	if first.CheckRedirect == nil || second.CheckRedirect == nil {
		t.Fatal("expected SSRF redirect policy to be set on both clients")
	}
}

// TestNewSSRFSafeHTTPClient_HasDedicatedTransport verifies the convenience
// constructor still builds a working transport + redirect policy.
func TestNewSSRFSafeHTTPClient_HasDedicatedTransport(t *testing.T) {
	client := NewSSRFSafeHTTPClient(DefaultSSRFSafeHTTPClientConfig())
	if client.Transport == nil {
		t.Fatal("expected a transport to be set")
	}
	guard, ok := client.Transport.(*SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating wrapper, got %T", client.Transport)
	}
	if _, ok := guard.Base.(*http.Transport); !ok {
		t.Fatalf("expected *http.Transport base, got %T", guard.Base)
	}
	if client.CheckRedirect == nil {
		t.Fatal("expected SSRF redirect policy to be set")
	}
}

func TestNewSSRFSafeTransport_TLSFallbackIsOptIn(t *testing.T) {
	transport := NewSSRFSafeTransport(DefaultSSRFSafeHTTPClientConfig())
	if transport.DialTLSContext != nil {
		t.Fatal("default transport must preserve callers that replace DialContext")
	}

	config := DefaultSSRFSafeHTTPClientConfig()
	config.EnableTLSFallback = true
	transport = NewSSRFSafeTransport(config)
	if transport.DialTLSContext == nil {
		t.Fatal("TLS-aware fallback must be enabled when explicitly requested")
	}
}
