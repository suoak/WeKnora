package config

import "github.com/spf13/viper"

// Register the opt-in key before decoding so AutomaticEnv can discover it
// even when an existing mounted configuration predates this option.
func registerPersonalMCPRanking(v *viper.Viper) {
	v.SetDefault("personal_mcp.compatible_global_ranks", false)
	v.SetDefault("personal_mcp.technical_api_routes", false)
}
