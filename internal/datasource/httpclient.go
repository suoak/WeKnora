package datasource

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
)

// ValidateConnectorBaseURL checks a connector API base URL against the SSRF policy.
// Empty rawURL is allowed; callers apply their own default before issuing requests.
func ValidateConnectorBaseURL(rawURL string) error {
	return ValidateConnectorBaseURLWithResolver(rawURL, nil)
}

// ValidateConnectorBaseURLWithResolver keeps preflight and pinned transport DNS
// consistent without relaxing the URL, whitelist, or address restrictions.
func ValidateConnectorBaseURLWithResolver(rawURL string, resolver *net.Resolver) error {
	url := strings.TrimSpace(rawURL)
	if url == "" {
		return nil
	}
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := utils.ValidateURLForSSRFWithResolver(ctx, url, resolver); err != nil {
		return fmt.Errorf("base_url SSRF validation failed: %w", err)
	}
	return nil
}

// NewConnectorHTTPClient returns an HTTP client with redirect and dial-time SSRF guards.
func NewConnectorHTTPClient(timeout time.Duration) *http.Client {
	return newConnectorHTTPClient(timeout, false)
}

// NewConnectorHTTPClientWithTLSFallback enables bounded TCP+TLS candidate
// fallback for connectors whose public HTTPS endpoint resolves to multiple
// interchangeable addresses. Other connector clients retain their established
// transport behavior through NewConnectorHTTPClient.
func NewConnectorHTTPClientWithTLSFallback(timeout time.Duration) *http.Client {
	return newConnectorHTTPClient(timeout, true)
}

// NewConnectorHTTPClientWithTLSResolver keeps the same SSRF and TLS policy
// while selecting a resolver only for this connector's pinned connections.
func NewConnectorHTTPClientWithTLSResolver(timeout time.Duration, resolver *net.Resolver) *http.Client {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout, cfg.EnableTLSFallback, cfg.DNSResolver = timeout, true, resolver
	return utils.NewSSRFSafeHTTPClient(cfg)
}

func newConnectorHTTPClient(timeout time.Duration, enableTLSFallback bool) *http.Client {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	cfg.EnableTLSFallback = enableTLSFallback
	return utils.NewSSRFSafeHTTPClient(cfg)
}
