package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSourceIDVerifiedEmailPolicyDefaultAndFailClosed(t *testing.T) {
	c := syntheticSSOConfig(t)
	require.Equal(t, "trusted_ownership", c.EffectiveSourceIDAccountLinkMode())
	c.SourceIDAccountScope = "enterprise_all"
	c.SourceIDPilotAccountsFile = ""
	c.SourceIDAccountLinkMode = "verified_email_policy"
	_, err := c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDVerifiedEmailPolicyConfirmed = true
	_, err = c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDVerifiedEmailPolicyRef = "synthetic-company-email-policy"
	_, err = c.SourceIDLocalPolicy()
	require.NoError(t, err)
	// No imported ownership/freshness or fabricated independent reference.
	require.Empty(t, c.SourceIDEnterprisePolicyRef)
	require.Empty(t, c.SourceIDEnterpriseMaxAge)
	for _, kind := range []string{"unknown", "pilot", "refresh", "auto", "contract", "userinfo", "policy", "sso"} {
		t.Run(kind, func(t *testing.T) {
			q := c
			switch kind {
			case "unknown":
				q.SourceIDAccountLinkMode = "unknown"
			case "pilot":
				q.SourceIDAccountScope = "pilot"
			case "refresh":
				q.SourceIDLocalRefreshEnable = true
			case "auto":
				q.SourceIDAutoBindingEnable = false
			case "contract":
				q.SourceIDContractConfirmed = false
			case "userinfo":
				q.SourceIDClaimsSource = "id_token"
			case "policy":
				q.SourceIDVerifiedEmailPolicyRef = " "
			case "sso":
				q.SourceIDSSOPolicyConfirmed = false
			}
			_, e := q.SourceIDLocalPolicy()
			require.Error(t, e)
		})
	}
	c.SourceIDAccountLinkMode = "trusted_ownership"
	_, err = c.SourceIDLocalPolicy()
	require.Error(t, err)
	t.Setenv("SOURCEID_ACCOUNT_LINK_MODE", "verified_email_policy")
	t.Setenv("SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED", "true")
	t.Setenv("SOURCEID_VERIFIED_EMAIL_POLICY_REF", "synthetic-env-policy")
	var empty OIDCAuthConfig
	applyOIDCEnvOverrides(&Config{OIDCAuth: &empty})
	require.True(t, empty.SourceIDVerifiedEmailPolicyConfirmed)
	require.Equal(t, "synthetic-env-policy", empty.SourceIDVerifiedEmailPolicyRef)
	require.Equal(t, "verified_email_policy", empty.SourceIDAccountLinkMode)
	require.False(t, empty.SourceIDContractConfirmed)
}
