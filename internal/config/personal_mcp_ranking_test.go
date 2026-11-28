package config

import (
	"strings"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestPersonalMCPCompatibleGlobalRanksEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, env string
		want            bool
	}{
		{"old_mounted_config_default", "personal_mcp:\n  enabled: true\n", "", false},
		{"old_mounted_config_opt_in", "personal_mcp:\n  enabled: true\n", "true", true},
		{"explicit_config", "personal_mcp:\n  enabled: true\n  compatible_global_ranks: true\n", "", true},
		{"environment_rollback", "personal_mcp:\n  enabled: true\n  compatible_global_ranks: true\n", "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PERSONAL_MCP_COMPATIBLE_GLOBAL_RANKS", tc.env)
			v := viper.New()
			registerPersonalMCPRanking(v)
			v.AutomaticEnv()
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			v.SetConfigType("yaml")
			require.NoError(t, v.ReadConfig(strings.NewReader(tc.yaml)))
			var cfg Config
			require.NoError(t, v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) { dc.TagName = "yaml" }))
			require.NotNil(t, cfg.PersonalMCP)
			require.True(t, cfg.PersonalMCP.Enabled)
			require.Equal(t, tc.want, cfg.PersonalMCP.CompatibleGlobalRanks)
		})
	}
}
