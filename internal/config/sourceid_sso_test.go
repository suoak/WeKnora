package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/privatefile"
	"github.com/stretchr/testify/require"
)

func syntheticSSOConfig(t *testing.T) OIDCAuthConfig {
	t.Helper()
	c := OIDCAuthConfig{IssuerURL: "https://mock.example", SourceIDEnable: true, SourceIDContractConfirmed: true, SourceIDEmailBindingEnable: true, SourceIDEmailContractConfirmed: true,
		SourceIDAutoBindingEnable: true, SourceIDAutoBindingPolicyRef: "synthetic-all-role", SourceIDSessionMode: "local_approved",
		SourceIDClaimsSource: "userinfo", SourceIDEmployeeClaim: "attributes.RJGH", SourceIDAdmissionMode: "oidc_sso_approved",
		SourceIDSSOPolicyConfirmed: true, SourceIDLocalPolicyRef: "synthetic-local", SourceIDAdmissionPolicyRef: "synthetic-sso",
		SourceIDLocalAccessTTL: "60m", SourceIDLocalAbsoluteTTL: "60m", SourceIDLocalIdleTTL: "30m", SourceIDAdmissionMaxAge: "60m"}
	c.SourceIDPilotAccountsFile = filepath.Join(t.TempDir(), "ownership.json")
	data, _ := json.Marshal(map[string]interface{}{"version": 1, "policy_ref": c.SourceIDAutoBindingPolicyRef, "ownership_ref": "synthetic-history-evidence", "issuer": c.IssuerURL, "accounts": []map[string]string{{"user_id": "synthetic-user", "email": " Owner@Example.Test ", "employee_no": "synthetic-employee"}}})
	require.NoError(t, privatefile.WriteFile(c.SourceIDPilotAccountsFile, data, 0600))
	return c
}

func TestSourceIDSSOPolicyFailsClosedAndPreservesForcedMode(t *testing.T) {
	c := syntheticSSOConfig(t)
	p, err := c.SourceIDLocalPolicy()
	require.NoError(t, err)
	require.Equal(t, "1h0m0s", p.AbsoluteTTL.String())
	accounts, err := c.SourceIDPilotAccounts()
	require.NoError(t, err)
	require.Equal(t, "owner@example.test", accounts["synthetic-user"].Email)
	for _, name := range []string{"approval", "manifest", "refresh", "prompt", "binding", "protocol", "contract", "policy"} {
		t.Run(name, func(t *testing.T) {
			cfg := c
			switch name {
			case "approval":
				cfg.SourceIDSSOPolicyConfirmed = false
			case "manifest":
				cfg.SourceIDPilotAccountsFile = ""
			case "refresh":
				cfg.SourceIDLocalRefreshEnable = true
			case "prompt":
				cfg.SourceIDForceReauthPromptLogin = true
			case "binding":
				cfg.SourceIDAutoBindingEnable = false
			case "protocol":
				cfg.SourceIDProtocolOnly = true
			case "contract":
				cfg.SourceIDContractConfirmed = false
			case "policy":
				cfg.SourceIDAutoBindingPolicyRef = ""
			}
			_, err := cfg.SourceIDLocalPolicy()
			require.Error(t, err)
		})
	}
	c.SourceIDAdmissionMode = "authorization"
	_, err = c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDAuthorizationRecheckConfirmed = true
	c.SourceIDForceReauthPromptLogin = true
	_, err = c.SourceIDLocalPolicy()
	require.NoError(t, err)
	var empty OIDCAuthConfig
	require.False(t, empty.SourceIDSSOPolicyConfirmed)
	applyOIDCEnvOverrides(&Config{OIDCAuth: &empty})
	// Loading a new boolean does not turn other gates on.
	t.Setenv("SOURCEID_SSO_POLICY_CONFIRMED", "true")
	t.Setenv("SOURCEID_PILOT_ACCOUNTS_FILE", c.SourceIDPilotAccountsFile)
	applyOIDCEnvOverrides(&Config{OIDCAuth: &empty})
	require.True(t, empty.SourceIDSSOPolicyConfirmed)
	require.False(t, empty.SourceIDContractConfirmed)
}

func TestSourceIDPilotManifestRejectsInvalidOwnership(t *testing.T) {
	for _, body := range []string{`{}`, `{"version":1,"policy_ref":"wrong","ownership_ref":"yes","accounts":[]}`, `{"version":1,"policy_ref":"synthetic-all-role","ownership_ref":"","accounts":[{"user_id":"u","email":"a@example.test"}]}`, `{"version":1,"policy_ref":"synthetic-all-role","ownership_ref":"evidence","accounts":[{"user_id":"u","email":"a@example.test"},{"user_id":"u","email":"b@example.test"}]}`, `{"version":1,"policy_ref":"synthetic-all-role","ownership_ref":"evidence","accounts":[{"user_id":"u","email":"a@example.test","role":"admin"}]}`} {
		c := syntheticSSOConfig(t)
		require.NoError(t, os.WriteFile(c.SourceIDPilotAccountsFile, []byte(body), 0600))
		_, err := c.SourceIDPilotAccounts()
		require.Error(t, err)
	}
}
