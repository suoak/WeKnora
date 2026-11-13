package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualSyncReturnsExistingRunningLogBeforeEnqueue(t *testing.T) {
	// Exercise DB admission independently of the separately tested egress gate.
	// This fixture has no Redis/controller or outbound connector dependency.
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false")
	f := newSQLiteDataSourceDeleteFixture(t)
	// No queue dependency is needed: a repeated request must never enqueue.
	svc := &DataSourceService{dsRepo: f.dsRepo, syncLogRepo: f.syncLogRepo}
	log, err := svc.ManualSync(context.Background(), f.ds.ID)
	require.NoError(t, err)
	require.NotNil(t, log)
	require.Equal(t, f.runningLog.ID, log.ID)
	logs, err := f.syncLogRepo.FindByDataSource(context.Background(), f.ds.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, logs, 2, "fixture's pending and running logs must be unchanged")
}
