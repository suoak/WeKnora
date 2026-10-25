package service

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2TestHTTPCallbackRequiresExactTemporaryIsolatedAllowlist(t *testing.T) {
	const callback = "http://172.30.232.42/feishu/callback"
	cfg := &FeishuAppConfig{
		AppID: "cli_synthetic", Secret: "synthetic-secret", RedirectURI: callback, Enabled: true,
		Capabilities: &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true},
	}
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	for _, key := range []string{
		"FEISHU_TEST_HTTP_CALLBACK_ENABLED", "KNOWHUB_ENVIRONMENT", "FEISHU_TEST_HTTP_CALLBACK_APP_ID",
		"FEISHU_TEST_HTTP_CALLBACK_URL", "FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT",
	} {
		t.Setenv(key, "")
	}
	require.Error(t, validateFeishuApp(cfg), "the default remains HTTPS")
	settings := map[string]string{
		"FEISHU_TEST_HTTP_CALLBACK_ENABLED":    "1",
		"KNOWHUB_ENVIRONMENT":                  "isolated-test",
		"FEISHU_TEST_HTTP_CALLBACK_APP_ID":     cfg.AppID,
		"FEISHU_TEST_HTTP_CALLBACK_URL":        callback,
		"FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	for key, value := range settings {
		t.Setenv(key, value)
	}
	require.NoError(t, validateFeishuApp(cfg))
	require.True(t, cfg.modeEnabled(types.FeishuUserOAuth))
	for key, value := range settings {
		t.Setenv(key, "")
		require.False(t, feishuTestHTTPCallbackAllowed(cfg), key)
		require.False(t, cfg.modeEnabled(types.FeishuUserOAuth), "runtime removal closes existing OAuth operations")
		require.True(t, cfg.modeEnabled(types.FeishuApplicationGrant), "the other identity remains independent")
		t.Setenv(key, value)
	}
	for _, raw := range []string{
		"http://172.30.232.43/feishu/callback", "http://172.30.232.42:8080/feishu/callback",
		"http://172.30.232.42/feishu/callback?x=1", "http://172.30.232.42/feishu/callback#x",
		"http://172.30.232.42/%66eishu/callback", "http://172.30.232.42/other",
	} {
		cfg.RedirectURI = raw
		require.Error(t, validateFeishuApp(cfg), "a different IP, port or path is never inherited")
	}
	for _, raw := range []string{
		"http://example.com/feishu/callback", "http://8.8.8.8/feishu/callback",
		"http://169.254.169.254/feishu/callback", "http://[fd00::1]/feishu/callback",
	} {
		cfg.RedirectURI = raw
		t.Setenv("FEISHU_TEST_HTTP_CALLBACK_URL", raw)
		require.Error(t, validateFeishuApp(cfg),
			"even an exact allowlist cannot authorize a public host or metadata endpoint")
	}
	cfg.RedirectURI = callback
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_URL", callback)
	for _, expiry := range []time.Time{time.Now().Add(-time.Minute), time.Now().Add(25 * time.Hour)} {
		t.Setenv("FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT", expiry.UTC().Format(time.RFC3339))
		require.Error(t, validateFeishuApp(cfg))
	}
	cfg.RedirectURI = "https://test.invalid/feishu/callback"
	require.NoError(t, validateFeishuApp(cfg), "production HTTPS validation remains independent")
}

func TestFeishuV2TestHTTPCallbackRemovalClosesPendingAuthorization(t *testing.T) {
	s, _ := feishuFixture(t)
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	cfg.Capabilities = &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true}
	cfg.RedirectURI = "http://172.30.232.42/feishu/callback"
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_ENABLED", "1")
	t.Setenv("KNOWHUB_ENVIRONMENT", "isolated-test")
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_APP_ID", cfg.AppID)
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_URL", cfg.RedirectURI)
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	admin := context.WithValue(personalUser("A"), types.SystemAdminContextKey, true)
	require.NoError(t, s.SaveApp(admin, cfg))
	authorization, err := s.Authorize(personalUser("A"))
	require.NoError(t, err)
	parsed, err := url.Parse(authorization)
	require.NoError(t, err)
	var calls atomic.Int32
	s.api = &http.Client{Transport: v2OAuthTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, nil
	})}
	t.Setenv("FEISHU_TEST_HTTP_CALLBACK_ENABLED", "0")
	require.Error(t, s.Complete(personalUser("A"), parsed.Query().Get("state"), "synthetic-unused-code"))
	require.Zero(t, calls.Load(), "a removed exception must close pending callbacks before token exchange")
}
