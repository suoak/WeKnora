package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDLocalSessionApprovalIsExplicit(t *testing.T) {
	var cfg OIDCAuthConfig
	require.Equal(t, "external", cfg.EffectiveSourceIDSessionMode())
	_, err := cfg.SourceIDLocalPolicy()
	require.Error(t, err)
	cfg = OIDCAuthConfig{
		SourceIDEnable: true, SourceIDContractConfirmed: true, SourceIDSessionMode: "local_approved",
		SourceIDEmployeeClaim: "attributes.RJGH",
		SourceIDClaimsSource:  "userinfo",
		SourceIDStatusClaim:   "mock_eligibility",

		SourceIDAllowedStatus: "ALLOW", SourceIDEmailBindingEnable: true, SourceIDEmailContractConfirmed: true,
		SourceIDLocalPolicyRef: "synthetic-approval", SourceIDAdmissionPolicyRef: "synthetic-sid-contract",
		SourceIDAdmissionMode: "userinfo_claim", SourceIDLocalAccessTTL: "10m", SourceIDLocalAbsoluteTTL: "8h",
		SourceIDLocalIdleTTL: "30m", SourceIDAdmissionMaxAge: "5m",
	}
	_, err = cfg.SourceIDLocalPolicy()
	require.NoError(t, err)
	for _, scenario := range []string{"protocol", "approval", "duration", "admission", "overlong", "unknown_mode"} {
		t.Run(scenario, func(t *testing.T) {
			c := cfg
			switch scenario {
			case "protocol":
				c.SourceIDProtocolOnly = true
			case "approval":
				c.SourceIDLocalPolicyRef = ""
			case "duration":
				c.SourceIDLocalIdleTTL = ""
			case "admission":
				c.SourceIDAdmissionMode = "token_200"
			case "overlong":
				c.SourceIDAdmissionMaxAge = "9h"
			case "unknown_mode":
				c.SourceIDSessionMode = "unapproved"
			}
			_, err := c.SourceIDLocalPolicy()
			require.Error(t, err)
		})
	}
}
