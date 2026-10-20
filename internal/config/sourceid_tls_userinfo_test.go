package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func tlsUserInfoConfig() *OIDCAuthConfig {
	return &OIDCAuthConfig{
		Enable:                     true,
		SourceIDEnable:             true,
		SourceIDContractConfirmed:  true,
		SourceIDValidationStrategy: SourceIDTLSUserInfoValidation,

		IssuerURL:            SourceIDPinnedIssuer,
		RedirectURI:          SourceIDHTTPCallback,
		ClientID:             "synthetic-client",
		ClientSecret:         "synthetic-secret",
		ClientAuthMethod:     "client_secret_basic",
		SourceIDClaimsSource: "userinfo",

		SourceIDTLSUserInfoConfirmed:   true,
		SourceIDTLSUserInfoApprovalRef: "SYNTHETIC-APPROVAL",
		SourceIDTLSUserInfoExpiresAt: time.Now().
			Add(
				time.Hour,
			).
			Format(
				time.RFC3339,
			),
	}
}

func TestSIDTLSUserInfoPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*OIDCAuthConfig)
		pass   bool
	}{
		{"valid", func(*OIDCAuthConfig) {}, true},
		{"disabled", func(c *OIDCAuthConfig) { c.Enable = false }, false},
		{"other_provider", func(c *OIDCAuthConfig) { c.SourceIDEnable = false }, false},
		{"other_issuer", func(c *OIDCAuthConfig) { c.IssuerURL = "https://other.example" }, false},
		{"protocol_only", func(c *OIDCAuthConfig) { c.SourceIDProtocolOnly = true }, false},
		{"no_approval", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoConfirmed = false }, false},
		{"no_approval_ref", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoApprovalRef = "" }, false},
		{"expired", func(c *OIDCAuthConfig) {
			c.SourceIDTLSUserInfoExpiresAt = time.Now().Add(-time.Second).Format(time.RFC3339)
		}, false},
		{"invalid_deadline", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoExpiresAt = "tomorrow" }, false},
		{"wrong_callback", func(c *OIDCAuthConfig) { c.RedirectURI = "http://other.example/callback" }, false},
		{"no_secret", func(c *OIDCAuthConfig) { c.ClientSecret = "" }, false},
		{"wrong_client_auth", func(c *OIDCAuthConfig) { c.ClientAuthMethod = "client_secret_post" }, false},
		{"id_token_employee_fallback", func(c *OIDCAuthConfig) { c.SourceIDClaimsSource = "id_token" }, false},
		{"wrong_discovery", func(c *OIDCAuthConfig) { c.DiscoveryURL = "https://other.example/discovery" }, false},
		{
			"wrong_token_endpoint",
			func(c *OIDCAuthConfig) { c.TokenEndpoint = "http://sid.ruijie.com.cn/oidc/accessToken" },
			false,
		},
		{
			"wrong_userinfo",
			func(c *OIDCAuthConfig) {
				c.UserInfoEndpoint = "https://sid.ruijie.com.cn/oidc/profile?override=1"
			},
			false,
		},
		{"wrong_transport", func(c *OIDCAuthConfig) { c.UserInfoTransport = "arbitrary" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tlsUserInfoConfig()
			tc.change(c)
			e := c.ValidateSourceIDValidationStrategy()
			if tc.pass {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
		})
	}
	c := &OIDCAuthConfig{}
	require.Equal(t, SourceIDJWKSValidation, c.EffectiveSourceIDValidationStrategy())
	require.NoError(t, c.ValidateSourceIDValidationStrategy())
}

func TestSIDTLSUserInfoEnvironment(t *testing.T) {
	t.Setenv("SOURCEID_TLS_USERINFO_CONFIRMED", "true")
	t.Setenv("SOURCEID_TLS_USERINFO_APPROVAL_REF", "SYNTHETIC-ENV")
	t.Setenv("SOURCEID_TLS_USERINFO_EXPIRES_AT", "2026-10-21T18:00:00+08:00")
	t.Setenv("SOURCEID_TLS_USERINFO_LIFECYCLE", "continuous_test")
	t.Setenv("SOURCEID_TLS_USERINFO_TEST_INSTANCE", "knowhub-mvp-p3@172.30.232.42")
	c := &Config{OIDCAuth: &OIDCAuthConfig{}}
	// The same existing environment loader is exercised by the repository's
	// config suite; this test keeps these names bound to the actual fields.
	applyOIDCEnvOverrides(c)
	require.True(t, c.OIDCAuth.SourceIDTLSUserInfoConfirmed)
	require.Equal(t, "SYNTHETIC-ENV", c.OIDCAuth.SourceIDTLSUserInfoApprovalRef)
	require.Equal(t, "2026-10-21T18:00:00+08:00", c.OIDCAuth.SourceIDTLSUserInfoExpiresAt)
	require.Equal(t, "continuous_test", c.OIDCAuth.SourceIDTLSUserInfoLifecycle)
	require.Equal(t, "knowhub-mvp-p3@172.30.232.42", c.OIDCAuth.SourceIDTLSUserInfoTestInstance)
}

func TestSIDTLSUserInfoContinuousTestPolicy(t *testing.T) {
	future, err := time.Parse(time.RFC3339, "2026-10-22T12:00:00+08:00")
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*OIDCAuthConfig)
		pass   bool
	}{
		{"after_previous_plan_deadline", func(*OIDCAuthConfig) {}, true},
		{"missing_instance", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoTestInstance = "" }, false},
		{"production_instance", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoTestInstance = "production" }, false},
		{"unknown_lifecycle", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoLifecycle = "unlimited" }, false},
		{"default_still_requires_deadline", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoLifecycle = "" }, false},
		{
			"time_limited_still_requires_deadline",
			func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoLifecycle = "time_limited" },
			false,
		},
		{
			"independent_approval_expiry_enforced",
			func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoExpiresAt = "2026-10-21T18:00:00+08:00" },
			false,
		},
		{"malformed_approval_expiry", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoExpiresAt = "never" }, false},
		{"no_confirmation", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoConfirmed = false }, false},
		{"no_approval_reference", func(c *OIDCAuthConfig) { c.SourceIDTLSUserInfoApprovalRef = "" }, false},
		{
			"http_token_endpoint",
			func(c *OIDCAuthConfig) { c.TokenEndpoint = "http://sid.ruijie.com.cn/oidc/accessToken" },
			false,
		},
		{"other_issuer", func(c *OIDCAuthConfig) { c.IssuerURL = "https://other.example" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tlsUserInfoConfig()
			c.SourceIDTLSUserInfoExpiresAt = ""
			c.SourceIDTLSUserInfoLifecycle = "continuous_test"
			c.SourceIDTLSUserInfoTestInstance = "knowhub-mvp-p3@172.30.232.42"
			tc.change(c)
			err := c.validateSIDTLSUserInfoPolicyAt(future)
			if tc.pass {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
