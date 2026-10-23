package datasource

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLegacyFeishuScopeP0(t *testing.T) {
	id, other := uuid.NewString(), uuid.NewString()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"", true},
		{"all", true},
		{id, true},
		{other, false},
		{"none", false},
		{other + ", " + id, true},
		{id + ",broken", false},
		{id + ",", false},
		{"*", false},
		{" ", false},
		{"{" + id + "}", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", tc.raw)
			for _, kind := range []string{
				types.ConnectorTypeFeishu, types.ConnectorTypeFeishuDrive,
				types.ConnectorTypeLark, types.ConnectorTypeLarkDrive,
			} {
				require.Equal(t, tc.want, LegacyFeishuSourceAllowed(kind, id))
			}
			require.True(t, LegacyFeishuSourceAllowed(types.ConnectorTypeRSS, id))
		})
	}
}

func TestLegacyFeishuScopeSchedulerSQLiteP0(t *testing.T) { legacyFeishuScopeSchedulerP0(t, "sqlite") }
func TestLegacyFeishuScopeSchedulerPostgresP0(t *testing.T) {
	legacyFeishuScopeSchedulerP0(t, "postgres")
}

func legacyFeishuScopeSchedulerP0(t *testing.T, dialect string) {
	// Isolate source-allowlist scheduling with an in-memory queue; the real
	// Redis/controller safety integration has its own enabled-mode tests.
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	sources, logs := repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db)
	allowed := uuid.NewString()
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", allowed)
	var blocked *types.DataSource
	for i, status := range []string{
		types.DataSourceStatusError, types.DataSourceStatusError, types.DataSourceStatusPaused,
	} {
		ds := &types.DataSource{
			ID:           uuid.NewString(),
			TenantID:     1,
			Type:         types.ConnectorTypeFeishuDrive,
			Status:       status,
			SyncSchedule: "0 0 0 * * *",
			Config:       types.JSON(`{}`),
		}
		if i == 0 {
			ds.ID = allowed
		}
		require.NoError(t, sources.Create(ctx, ds))
		if i == 1 {
			blocked = ds
		}
	}
	q := &fakeTaskEnqueuer{}
	s := NewScheduler(sources, logs, q)
	require.NoError(t, s.Start(ctx))
	defer s.Stop()
	require.Equal(t, 1, s.EntryCount())
	require.NoError(t, s.AddOrUpdate(blocked))
	s.triggerSync(blocked.ID, 1)
	require.Zero(t, q.count.Load())
	s.triggerSync(allowed, 1)
	require.EqualValues(t, 1, q.count.Load())
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", "none")
	// Even a registered tick must recheck deployment scope.
	s.triggerSync(allowed, 1)
	require.EqualValues(t, 1, q.count.Load())
	saved, err := sources.FindByID(ctx, blocked.ID)
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusError, saved.Status)
	require.Equal(t, blocked.SyncSchedule, saved.SyncSchedule)
}
