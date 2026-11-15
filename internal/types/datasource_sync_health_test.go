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
		{"active", "error", "temporary network failure", "failed", "needs_attention"},
		{"active", "active", "", "success", "healthy"},
	} {
		ds := &DataSource{Type: "feishu", LegacyAccessState: tc.state, Status: tc.status, ErrorMessage: tc.error}
		ds.LatestSyncLog = &SyncLog{Status: tc.run}
		require.Equal(t, tc.code, ds.DescribeSyncHealth().Code)
	}
	require.Nil(t, (&DataSource{Type: "gitlab"}).DescribeSyncHealth())
}

func TestFeishuSyncHealthReportsDurableAutomaticRecovery(t *testing.T) {
	ds := &DataSource{Type: "feishu", Status: DataSourceStatusActive, LegacyAccessState: "active"}
	ds.LatestSyncLog = &SyncLog{Status: SyncLogStatusSuccess, Result: JSON(`{"auto_recovered":1}`)}
	health := ds.DescribeSyncHealth()
	require.Equal(t, "healthy", health.Code)
	require.Equal(t, "view_progress", health.Action)
	ds.Status = DataSourceStatusError
	require.Equal(t, "retrying", ds.DescribeSyncHealth().Code, "an old recovery cannot hide a new source error")
}
