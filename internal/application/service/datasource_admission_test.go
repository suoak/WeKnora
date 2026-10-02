package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/stretchr/testify/require"
)

func TestManualSyncRejectsExistingRunningLogBeforeEnqueue(t *testing.T) {
	f := newSQLiteDataSourceDeleteFixture(t)
	// No queue dependency is needed: a rejected admission must never enqueue.
	svc := &DataSourceService{dsRepo: f.dsRepo, syncLogRepo: f.syncLogRepo}
	log, err := svc.ManualSync(context.Background(), f.ds.ID)
	require.ErrorIs(t, err, datasource.ErrSyncAlreadyRunning)
	require.Nil(t, log)
	logs, err := f.syncLogRepo.FindByDataSource(context.Background(), f.ds.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, logs, 2, "fixture's pending and running logs must be unchanged")
}
