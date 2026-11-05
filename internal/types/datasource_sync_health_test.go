package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeishuSyncHealthSeparatesUserActionFromDocumentFailures(t *testing.T) {
	for _, tc := range []struct {
		state, status, error, run, code string
	}{
		{"revoked", "paused", "", "", "paused"},
		{"revoked", "error", "", "", "reauthorize"},
		{"hold", "error", "Feishu credential reference unavailable", "failed", "reauthorize"},
		{"hold", "error", "historical inventory unverified", "failed", "verification_required"},
		{"active", "active", "", "partial", "partial"},
		{"active", "active", "", "running", "retrying"},
		{"active", "error", "temporary network failure", "failed", "retrying"},
		{"active", "active", "", "success", "healthy"},
	} {
		ds := &DataSource{Type: "feishu", LegacyAccessState: tc.state, Status: tc.status, ErrorMessage: tc.error}
		ds.LatestSyncLog = &SyncLog{Status: tc.run}
		require.Equal(t, tc.code, ds.DescribeSyncHealth().Code)
	}
	require.Nil(t, (&DataSource{Type: "gitlab"}).DescribeSyncHealth())
}
