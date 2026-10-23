package feishusafety

import "time"

// DefaultConfig is a conservative shared application budget, not a statement
// about upstream quota or remaining account credit. Explicit JSON may override it.
// UTF-8 bytes conservatively bound text-embedding input tokens; overhead reserves
// framing per input. Inputs that cannot fit are refused rather than undercounted.
func DefaultConfig() Config {
	return Config{Namespace: "legacy-feishu", EndpointHost: "uniapi.ruijie.com.cn",
		InFlight: 1, RPM: 30, RPS: 1, TPM: 30000, WaitBudgetDuration: 65 * time.Second,
		TokenBoundVerified: true, TokenOverhead: 128}
}
