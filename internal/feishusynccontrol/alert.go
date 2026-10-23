package feishusynccontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// NewAlertSink constructs an acknowledged, HTTPS event receiver. A nil client
// uses the existing SSRF-safe client; tests may provide a pinned local TLS fixture.
// It does not send any event until invoked and never includes credentials/content.
func NewAlertSink(endpoint string, client *http.Client) (func(Event) error, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("explicit HTTPS alert receiver required")
	}
	if client == nil {
		cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
		cfg.Timeout = time.Second
		client = secutils.NewSSRFSafeHTTPClient(cfg)
	}
	return func(event Event) error {
		body, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		request, requestErr := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		response, sendErr := client.Do(request)
		if sendErr != nil {
			return fmt.Errorf("alert receiver unavailable")
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("alert receiver rejected event: status=%d", response.StatusCode)
		}
		return nil
	}, nil
}
