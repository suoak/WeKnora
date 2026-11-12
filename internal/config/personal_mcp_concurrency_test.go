package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPersonalMCPConcurrencyConfig(t *testing.T) {
	c, err := (PersonalMCPConcurrencyConfig{}).Resolve()
	require.NoError(t, err)
	require.Equal(t, 16, c.GlobalLimit)
	require.Equal(t, 4, c.UserLimit)
	require.Equal(t, 4, c.ProtocolUserLimit)
	require.Equal(t, 1500, c.WaitTimeoutMs)
	for _, bad := range []PersonalMCPConcurrencyConfig{
		{GlobalLimit: -1}, {UserLimit: 17}, {QueueLimit: 1}, {WaitTimeoutMs: 10000}, {ProtocolLimit: 1},
	} {
		_, err := bad.Resolve()
		require.Error(t, err)
	}
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(`personal_mcp:
  concurrency:
    global_limit: 24
    user_limit: 6
    wait_timeout_ms: 2000
`), &cfg))
	c, err = cfg.PersonalMCP.Concurrency.Resolve()
	require.NoError(t, err)
	require.Equal(t, 24, c.GlobalLimit)
	require.Equal(t, 6, c.UserLimit)
	require.Equal(t, 2000, c.WaitTimeoutMs)
}
