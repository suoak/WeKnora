package feishusafety

import (
	"context"
	"testing"
)

func TestSafetyActivationAndEmergency(t *testing.T) {
	for _, value := range []string{"true", "invalid"} {
		t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", value)
		InstallDefault(nil)
		if CheckLegacy(context.Background(), "feishu_drive", "source") == nil {
			t.Fatal("enabled or malformed activation must never silently bypass a missing guard")
		}
		if CheckLegacy(context.Background(), "rss", "source") != nil {
			t.Fatal("unrelated connector affected")
		}
	}
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false")
	if CheckLegacy(context.Background(), "feishu", "source") != nil {
		t.Fatal("explicitly disabled mode changed accepted P0 compatibility")
	}
	g, client := fixture(t, Config{})
	if err := EmergencyBlock(context.Background(), client, g.config.Namespace); err != nil {
		t.Fatal(err)
	}
	if err := g.Check(context.Background()); err == nil {
		t.Fatal("emergency command failed to block provider-independent outbound")
	}
	if client.Exists(context.Background(), "asynq:{sync}:paused").Val() != 1 {
		t.Fatal("emergency stop did not pause sync")
	}
	if err := EmergencyBlock(context.Background(), client, g.config.Namespace); err != nil {
		t.Fatal("stop must be idempotent", err)
	}
	// Remove only the test fixture's pause flag in its isolated Redis database.
	if err := client.Del(context.Background(), "asynq:{sync}:paused").Err(); err != nil {
		t.Fatal(err)
	}
}
