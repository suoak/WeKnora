package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDForcedPromptEnvironmentDefaultsOff(t *testing.T) {
	t.Setenv("SOURCEID_FORCE_REAUTH_PROMPT_LOGIN", "")
	c := Config{OIDCAuth: &OIDCAuthConfig{}}
	applyOIDCEnvOverrides(&c)
	require.False(t, c.OIDCAuth.SourceIDForceReauthPromptLogin)
	t.Setenv("SOURCEID_FORCE_REAUTH_PROMPT_LOGIN", "true")
	applyOIDCEnvOverrides(&c)
	require.True(t, c.OIDCAuth.SourceIDForceReauthPromptLogin)
	t.Setenv("SOURCEID_FORCE_REAUTH_PROMPT_LOGIN", "false")
	applyOIDCEnvOverrides(&c)
	require.False(t, c.OIDCAuth.SourceIDForceReauthPromptLogin)
}

func TestSourceIDAuthorizationLocalPolicyRequiresForcedPrompt(t *testing.T) {
	c := OIDCAuthConfig{
		SourceIDEnable: true, SourceIDContractConfirmed: true, SourceIDSessionMode: "local_approved",
		SourceIDEmailBindingEnable:     true,
		SourceIDEmailContractConfirmed: true,
		SourceIDEmployeeClaim:          "attributes.RJGH",
		SourceIDClaimsSource:           "userinfo",

		SourceIDLocalPolicyRef:     "synthetic-approval",
		SourceIDAdmissionPolicyRef: "synthetic-sid-contract",
		SourceIDAdmissionMode:      "authorization",

		SourceIDLocalAccessTTL:   "10m",
		SourceIDLocalAbsoluteTTL: "10m",
		SourceIDLocalIdleTTL:     "10m",
		SourceIDAdmissionMaxAge:  "10m",
	}
	_, err := c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDAuthorizationRecheckConfirmed = true
	_, err = c.SourceIDLocalPolicy()
	require.ErrorContains(t, err, "forced reauthentication")
	c.SourceIDForceReauthPromptLogin = true
	_, err = c.SourceIDLocalPolicy()
	require.NoError(t, err)
	c.SourceIDLocalRefreshEnable = true
	_, err = c.SourceIDLocalPolicy()
	require.ErrorContains(t, err, "refresh disabled")
}
