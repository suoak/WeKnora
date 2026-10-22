package service

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEnterpriseCapabilityIsExplicitAndRequiresRetrieve(t *testing.T) {
	defaults, err := normalizeUserMCPCapabilities(nil, true)
	require.NoError(t, err)
	require.NotContains(t, defaults, "enterprise_retrieve")
	_, err = normalizeUserMCPCapabilities([]string{"enterprise_retrieve"}, false)
	require.Error(t, err)
	explicit, err := normalizeUserMCPCapabilities([]string{"retrieve", "enterprise_retrieve"}, false)
	require.NoError(t, err)
	require.Contains(t, explicit, string(types.APIKeyCapabilityEnterpriseRetrieve))
	old, err := normalizeUserMCPCapabilities([]string{"retrieve"}, false)
	require.NoError(t, err)
	require.Equal(t, types.StringArray{"retrieve"}, old)
}
