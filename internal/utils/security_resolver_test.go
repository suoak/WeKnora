package utils

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// resolverFixture answers real Go resolver packets without public networking.
func resolverFixture(answers map[string][]string, calls *atomic.Int32) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			var size [2]byte
			if _, err := io.ReadFull(server, size[:]); err != nil {
				return
			}
			wire := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(server, wire); err != nil {
				return
			}
			var message dnsmessage.Message
			if message.Unpack(wire) != nil || len(message.Questions) != 1 {
				return
			}
			question := message.Questions[0]
			message.Response, message.RecursionAvailable = true, true
			if question.Type == dnsmessage.TypeA {
				for _, raw := range answers[strings.TrimSuffix(question.Name.String(), ".")] {
					ip := net.ParseIP(raw).To4()
					message.Answers = append(message.Answers, dnsmessage.Resource{
						Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1},
						Body:   &dnsmessage.AResource{A: [4]byte{ip[0], ip[1], ip[2], ip[3]}},
					})
				}
			}
			wire, err := message.Pack()
			if err != nil {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(wire)))
			_, _ = server.Write(append(size[:], wire...))
		}()
		return client, nil
	}}
}

type resolverRoundTrip func(*http.Request) (*http.Response, error)

func (f resolverRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSSRFConnectorResolverCoversRequestAndRedirectPolicy(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "192.168.250.0/24")
	t.Setenv("SSRF_DNS_WHITELIST_ONLY", "false")
	ResetSSRFWhitelistForTest()
	t.Cleanup(ResetSSRFWhitelistForTest)
	previous := net.DefaultResolver
	var systemCalls atomic.Int32
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		systemCalls.Add(1)
		return nil, errors.New("system DNS unavailable")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })

	for _, test := range []struct {
		name      string
		addresses []string
		redirect  bool
		allowed   bool
	}{
		{"public", []string{"8.8.4.4"}, false, true},
		{"public_redirect", []string{"8.8.4.4"}, true, true},
		{"private", []string{"127.0.0.1"}, false, false},
		{"mixed", []string{"8.8.4.4", "10.0.0.1"}, false, false},
		{"private_redirect", []string{"169.254.169.254"}, true, false},
		{"empty_answer", nil, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ResetSSRFOutboundValidationCacheForTest()
			var dnsCalls atomic.Int32
			answers := map[string][]string{"connector.invalid": test.addresses}
			if test.redirect {
				answers = map[string][]string{"connector.invalid": {"8.8.8.8"}, "redirect.invalid": test.addresses}
			}
			cfg := DefaultSSRFSafeHTTPClientConfig()
			cfg.Timeout, cfg.DNSResolver = time.Second, resolverFixture(answers, &dnsCalls)
			requests := 0
			client := NewSSRFSafeHTTPClientWithTransport(cfg, resolverRoundTrip(func(r *http.Request) (*http.Response, error) {
				requests++
				response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}
				if test.redirect && r.URL.Hostname() == "connector.invalid" {
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", "https://redirect.invalid/final")
				} else if test.redirect && r.Header.Get("Authorization") != "" {
					t.Fatal("redirect leaked credentials")
				}
				return response, nil
			}))
			request, err := http.NewRequest(http.MethodGet, "https://connector.invalid/start", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer fixture-only")
			response, err := client.Do(request)
			if response != nil {
				_ = response.Body.Close()
			}
			if test.allowed && err != nil {
				t.Fatalf("configured safe resolver was ignored: %v", err)
			}
			if !test.allowed && err == nil {
				t.Fatal("unsafe DNS answer admitted")
			}
			wantRequests := 0
			if test.allowed {
				wantRequests = 1
			}
			if test.redirect {
				wantRequests++
			}
			if requests != wantRequests || dnsCalls.Load() == 0 {
				t.Fatalf("unexpected request/resolver counts: requests=%d want=%d dns=%d", requests, wantRequests, dnsCalls.Load())
			}
		})
	}
	if systemCalls.Load() != 0 {
		t.Fatalf("configured client consulted system DNS %d times", systemCalls.Load())
	}
}

func TestSSRFConnectorResolverDoesNotShareSuccessOrRelaxPolicy(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "")
	t.Setenv("SSRF_DNS_WHITELIST_ONLY", "false")
	ResetSSRFWhitelistForTest()
	t.Cleanup(ResetSSRFWhitelistForTest)
	for _, test := range []struct {
		name string
		url  string
		ips  []string
	}{
		{"public", "https://connector.invalid/first", []string{"8.8.8.8"}},
		{"same_origin_private_resolver", "https://connector.invalid/second", []string{"10.0.0.1"}},
		{"direct_ip", "https://8.8.8.8/file", []string{"8.8.8.8"}},
		{"restricted_port", "https://connector.invalid:22/file", []string{"8.8.8.8"}},
		{"scheme", "file://connector.invalid/file", []string{"8.8.8.8"}},
		{"whitelist_only", "https://connector.invalid/file", []string{"8.8.8.8"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "whitelist_only" {
				t.Setenv("SSRF_DNS_WHITELIST_ONLY", "true")
			}
			var calls atomic.Int32
			cfg := DefaultSSRFSafeHTTPClientConfig()
			cfg.Timeout, cfg.DNSResolver = time.Second, resolverFixture(map[string][]string{"connector.invalid": test.ips}, &calls)
			reached := false
			client := NewSSRFSafeHTTPClientWithTransport(cfg, resolverRoundTrip(func(r *http.Request) (*http.Response, error) {
				reached = true
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
			}))
			response, err := client.Get(test.url)
			if response != nil {
				_ = response.Body.Close()
			}
			if test.name == "public" {
				if err != nil || !reached {
					t.Fatalf("safe configured resolver rejected: %v", err)
				}
			} else if err == nil || reached {
				t.Fatalf("unsafe target or cross-resolver cached success admitted: err=%v reached=%v", err, reached)
			}
			if test.name == "whitelist_only" && calls.Load() != 0 {
				t.Fatal("whitelist-only policy queried DNS")
			}
		})
	}
}
