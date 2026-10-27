package service

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func internalHTTPConfig(t *testing.T) *FeishuAppConfig {
	t.Helper()
	cfg := &FeishuAppConfig{
		AppID:        "cli_synthetic",
		Secret:       "synthetic-only",
		Enabled:      true,
		RedirectURI:  "http://kb.internal.example/feishu/callback",
		Capabilities: &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true},
	}
	for key, value := range map[string]string{
		"KNOWHUB_ENVIRONMENT": "production", "FEISHU_ACCESS_V2_ENABLED": "1",
		"FEISHU_INTERNAL_HTTP_CALLBACK_ENABLED": "1", "FEISHU_INTERNAL_HTTP_CALLBACK_APP_ID": cfg.AppID,
		"FEISHU_INTERNAL_HTTP_CALLBACK_URL": cfg.RedirectURI, "FEISHU_INTERNAL_HTTP_CALLBACK_IPV4": "10.20.30.40",
		"FEISHU_INTERNAL_HTTP_CALLBACK_APPROVAL_REF": "operator:synthetic-approved-policy",
	} {
		t.Setenv(key, value)
	}
	return cfg
}

func TestFeishuV2InternalHTTPRequiresIndependentExactDeploymentPolicy(t *testing.T) {
	cfg := internalHTTPConfig(t)
	require.NoError(t, validateFeishuApp(cfg))
	require.True(t, cfg.modeEnabled(types.FeishuUserOAuth))
	require.False(t, feishuTestHTTPCallbackAllowed(cfg))
	for _, key := range []string{
		"KNOWHUB_ENVIRONMENT", "FEISHU_INTERNAL_HTTP_CALLBACK_ENABLED", "FEISHU_INTERNAL_HTTP_CALLBACK_APP_ID",
		"FEISHU_INTERNAL_HTTP_CALLBACK_URL", "FEISHU_INTERNAL_HTTP_CALLBACK_IPV4",
		"FEISHU_INTERNAL_HTTP_CALLBACK_APPROVAL_REF",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			require.Error(t, validateFeishuApp(cfg))
			require.False(t, cfg.modeEnabled(types.FeishuUserOAuth))
			require.True(t, cfg.modeEnabled(types.FeishuApplicationGrant))
		})
	}
	for _, callback := range []string{
		"http://other.internal.example/feishu/callback", "http://kb.internal.example:8080/feishu/callback",
		"http://kb.internal.example/feishu/callback?code=synthetic", "http://kb.internal.example/%66eishu/callback",
		"http://kb.internal.example/other", "http://kb.internal.example./feishu/callback",
		"http://10.20.30.40/feishu/callback",
	} {
		cfg.RedirectURI = callback
		require.Error(t, validateFeishuApp(cfg))
	}
}

func TestFeishuV2InternalHTTPRechecksPinnedDNSBeforeOAuth(t *testing.T) {
	cfg := internalHTTPConfig(t)
	for _, tc := range []struct {
		name      string
		addresses []netip.Addr
		err       error
		allowed   bool
	}{
		{name: "pinned", addresses: []netip.Addr{netip.MustParseAddr("10.20.30.40")}, allowed: true},
		{name: "public", addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
		{name: "changed_private", addresses: []netip.Addr{netip.MustParseAddr("10.20.30.41")}},
		{name: "mixed", addresses: []netip.Addr{netip.MustParseAddr("10.20.30.40"), netip.MustParseAddr("8.8.8.8")}},
		{name: "empty"},
		{name: "resolver_failed", err: errors.New("synthetic resolver failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyFeishuInternalHTTPCallbackWithLookup(
				context.Background(),
				cfg,
				func(ctx context.Context, network, host string) ([]netip.Addr, error) {
					require.Equal(t, "ip", network)
					require.Equal(t, "kb.internal.example", host)
					_, hasDeadline := ctx.Deadline()
					require.True(t, hasDeadline)
					return tc.addresses, tc.err
				},
			)
			require.Equal(t, tc.allowed, err == nil)
		})
	}
	for _, pin := range []string{"8.8.8.8", "127.0.0.1", "169.254.169.254", "fd00::1", ""} {
		t.Setenv("FEISHU_INTERNAL_HTTP_CALLBACK_IPV4", pin)
		require.False(t, feishuInternalHTTPCallbackAllowed(cfg))
	}
}
