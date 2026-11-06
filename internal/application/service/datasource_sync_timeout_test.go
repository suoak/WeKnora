package service

import (
	"testing"
	"time"
)

func TestDataSourceSyncTimeoutKeepsOtherSourcesBounded(t *testing.T) {
	for _, raw := range []string{
		``, `invalid`, `{"large":"0"}`, `{"large":"-1h"}`,
		`{"large":"1h"}`, `{"large":"25h"}`, `{"large":86400}`,
	} {
		t.Setenv("DATASOURCE_SYNC_TIMEOUT_OVERRIDES_JSON", raw)
		if got := dataSourceSyncTimeout("large"); got != 2*time.Hour {
			t.Fatalf("invalid override changed timeout to %v", got)
		}
	}
	t.Setenv("DATASOURCE_SYNC_TIMEOUT_OVERRIDES_JSON", `{"large":"24h"}`)
	if dataSourceSyncTimeout("large") != 24*time.Hour {
		t.Fatal("bounded large-source budget was not applied")
	}
	if dataSourceSyncTimeout("ordinary") != 2*time.Hour {
		t.Fatal("a large source changed an unrelated source's timeout")
	}
}
