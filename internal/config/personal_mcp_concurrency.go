package config

import "fmt"

// PersonalMCPConcurrencyConfig limits each APP instance; all personal routes share its scheduler.
// Protocol capacity is reserved separately so a slow search cannot block discovery.
type PersonalMCPConcurrencyConfig struct {
	GlobalLimit       int `yaml:"global_limit" json:"global_limit"`
	UserLimit         int `yaml:"user_limit" json:"user_limit"`
	QueueLimit        int `yaml:"queue_limit" json:"queue_limit"`
	UserQueueLimit    int `yaml:"user_queue_limit" json:"user_queue_limit"`
	WaitTimeoutMs     int `yaml:"wait_timeout_ms" json:"wait_timeout_ms"`
	ProtocolLimit     int `yaml:"protocol_limit" json:"protocol_limit"`
	ProtocolUserLimit int `yaml:"protocol_user_limit" json:"protocol_user_limit"`
	StreamLimit       int `yaml:"stream_limit" json:"stream_limit"`
	StreamUserLimit   int `yaml:"stream_user_limit" json:"stream_user_limit"`
}

// Resolve applies defaults without allowing zero/negative values to disable protection.
func (c PersonalMCPConcurrencyConfig) Resolve() (PersonalMCPConcurrencyConfig, error) {
	fields := []struct {
		value             *int
		fallback, maximum int
	}{
		{&c.GlobalLimit, 16, 256},
		{&c.UserLimit, 4, 64},
		{&c.QueueLimit, 64, 4096},
		{&c.UserQueueLimit, 8, 256},
		{&c.WaitTimeoutMs, 1500, 5000},
		{&c.ProtocolLimit, 8, 64},
		{&c.ProtocolUserLimit, 4, 16},
		{&c.StreamLimit, 8, 64},
		{&c.StreamUserLimit, 2, 16},
	}
	for _, f := range fields {
		if *f.value == 0 {
			*f.value = f.fallback
		}
		if *f.value < 1 || *f.value > f.maximum {
			return c, fmt.Errorf("invalid personal MCP concurrency limit")
		}
	}
	if c.UserLimit > c.GlobalLimit || c.UserQueueLimit > c.QueueLimit ||
		c.ProtocolUserLimit > c.ProtocolLimit || c.StreamUserLimit > c.StreamLimit {
		return c, fmt.Errorf("personal MCP user limits exceed global limits")
	}
	return c, nil
}
