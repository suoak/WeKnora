package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseCapabilityIsExplicitAndRetainsPersonalBuiltins(t *testing.T) {
	defaults, err := normalizeUserMCPCapabilities(nil, true)
	require.NoError(t, err)
	require.NotContains(t, defaults, "enterprise_retrieve")
	withEnterprise, err := normalizeUserMCPCapabilities([]string{"enterprise_retrieve"}, false)
	require.NoError(t, err)
	require.ElementsMatch(t, types.StringArray{"enterprise_retrieve", "retrieve", "add_content"}, withEnterprise)
	explicit, err := normalizeUserMCPCapabilities([]string{"retrieve", "enterprise_retrieve"}, false)
	require.NoError(t, err)
	require.Contains(t, explicit, string(types.APIKeyCapabilityEnterpriseRetrieve))
	old, err := normalizeUserMCPCapabilities([]string{"retrieve"}, false)
	require.NoError(t, err)
	require.Equal(t, types.StringArray{"retrieve", "add_content"}, old)
	require.NotContains(t, old, "enterprise_retrieve")
}
