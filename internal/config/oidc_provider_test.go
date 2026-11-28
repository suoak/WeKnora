package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOIDCProviderFailureIsolatedFromApplicationConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*OIDCAuthConfig)
	}{
		{"expired", func(c *OIDCAuthConfig) {
			c.SourceIDTLSUserInfoExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
		}},
		{"invalid_expiry", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoExpiresAt = "invalid" }},
		{"missing_approval", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoApprovalRef = "" }},
		{"missing_client_secret", func(c *OIDCAuthConfig) { c.ClientSecret = "" }},
		{"invalid_basic_encoding", func(c *OIDCAuthConfig) { c.SourceIDBasicEncoding = "unsupported" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := tlsUserInfoConfig()
			provider.DiscoveryURL = SourceIDPinnedIssuer + "/oidc/.well-known/openid-configuration"
			cfg := &Config{OIDCAuth: provider}
			require.NoError(t, ValidateConfig(cfg))
			tc.change(provider)
			original := *provider
			require.Error(t, ValidateOIDCProviderConfig(provider))
			require.Error(t, ValidateConfig(cfg), "strict release/config preflight must still reject")
			require.NoError(t, ValidateApplicationConfig(cfg), "unrelated services must remain restartable")
			require.Equal(t, original, *provider, "never disable gates or rewrite approval to allow startup")
			cfg.Server = &ServerConfig{Port: 0}
			require.Error(t, ValidateApplicationConfig(cfg), "core configuration errors remain fatal")
		})
	}
}
