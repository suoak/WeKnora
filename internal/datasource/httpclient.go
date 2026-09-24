package datasource

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
)

// ValidateConnectorBaseURL checks a connector API base URL against the SSRF policy.
// Empty rawURL is allowed; callers apply their own default before issuing requests.
func ValidateConnectorBaseURL(rawURL string) error {
	url := strings.TrimSpace(rawURL)
	if url == "" {
		return nil
	}
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	if err := utils.ValidateURLForSSRF(url); err != nil {
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

func newConnectorHTTPClient(timeout time.Duration, enableTLSFallback bool) *http.Client {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	cfg.EnableTLSFallback = enableTLSFallback
	return utils.NewSSRFSafeHTTPClient(cfg)
}
