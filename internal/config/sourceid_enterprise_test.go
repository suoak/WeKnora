package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDEnterpriseScopeFailsClosedAndKeepsPilot(t *testing.T) {
	c := syntheticSSOConfig(t)
	require.Equal(t, "pilot", c.EffectiveSourceIDAccountScope())
	c.SourceIDAccountScope = "enterprise_all"
	c.SourceIDPilotAccountsFile = ""
	_, err := c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDEnterprisePolicyConfirmed = true
	c.SourceIDEnterprisePolicyRef = "synthetic-enterprise"
	c.SourceIDEnterpriseMaxAge = "1h"
	_, err = c.SourceIDLocalPolicy()
	require.NoError(t, err)
	c.SourceIDAccountScope = "unknown"
	_, err = c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDAccountScope = "enterprise_all"
	c.SourceIDJITEnable = true
	_, err = c.SourceIDLocalPolicy()
	require.Error(t, err)
	c.SourceIDJITPolicyConfirmed = true
	c.SourceIDJITPolicyRef = "synthetic-jit"
	c.SourceIDJITEmailDomains = "example.test"
	_, err = c.SourceIDLocalPolicy()
	require.NoError(t, err)
	for _, domain := range []string{"", "*.example.test", "example.test/evil", "employee@example.test"} {
		c.SourceIDJITEmailDomains = domain
		_, err = c.SourceIDLocalPolicy()
		require.Error(t, err)
	}
}
