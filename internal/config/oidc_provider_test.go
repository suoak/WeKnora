package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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

func TestOIDCExpiredProviderDoesNotPreventConfigLoading(t *testing.T) {
	provider := tlsUserInfoConfig()
	provider.DiscoveryURL = SourceIDPinnedIssuer + "/oidc/.well-known/openid-configuration"
	provider.SourceIDTLSUserInfoExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339)
	raw, err := yaml.Marshal(&Config{
		Server: &ServerConfig{Port: 8080}, OIDCAuth: provider,
		KnowledgeBase: &KnowledgeBaseConfig{ChunkSize: 512, ChunkOverlap: 64},
	})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	viper.Reset()
	t.Cleanup(viper.Reset)
	originalConfigDir := resolvedConfigDir
	t.Cleanup(func() { resolvedConfigDir = originalConfigDir })
	t.Chdir(filepath.Dir(path))
	loaded, err := LoadConfig()
	require.NoError(t, err, "the actual startup loader must isolate expired optional provider policy")
	require.True(t, loaded.OIDCAuth.Enable)
	require.Equal(t, provider.SourceIDTLSUserInfoExpiresAt, loaded.OIDCAuth.SourceIDTLSUserInfoExpiresAt)
	require.Error(t, ValidateConfig(loaded), "strict release validation still rejects the unchanged expiry")
}
