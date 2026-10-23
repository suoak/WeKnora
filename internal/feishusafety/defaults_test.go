package feishusafety

import (
	"testing"
	"time"
)

func TestDefaultSharedBudgetConfig(t *testing.T) {
	config := DefaultConfig()
	if config.InFlight != 1 || config.RPS != 1 || config.RPM != 30 || config.TPM != 30000 || config.WaitBudgetDuration != 65*time.Second || !config.TokenBoundVerified {
		t.Fatal("conservative default allocation changed", config)
	}
	_, client := fixture(t, Config{})
	guard, err := New(client, config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(client, DefaultConfig())
	if err != nil || guard.Fingerprint() != second.Fingerprint() || guard.PermitKey() != second.PermitKey() {
		t.Fatal("workers would use different default allocations", err)
	}
}
