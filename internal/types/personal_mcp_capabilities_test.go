package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalMCPBuiltinsArePersistenceDefaultsNotReadGrants(t *testing.T) {
	input := StringArray{"enterprise_retrieve", "retrieve"}
	before := append(StringArray(nil), input...)
	got := WithPersonalMCPBuiltinCapabilities(input)
	require.Equal(t, before, input)
	require.Equal(t, StringArray{"enterprise_retrieve", "retrieve", "add_content"}, got)
	require.Equal(t, got, WithPersonalMCPBuiltinCapabilities(got))
	require.Equal(t, StringArray{"retrieve"}, NormalizeAPIKeyCapabilities(StringArray{"retrieve"}))
	require.False(t, (TenantAPIKeyScope{Capabilities: input}).HasCapability(APIKeyCapabilityAddContent),
		"stored legacy rows must be repaired explicitly, not elevated at authentication time")
}
