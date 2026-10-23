package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestReleaseSettingsSQLiteP0(t *testing.T)   { releaseSettings(t, "sqlite") }
func TestReleaseSettingsPostgresP0(t *testing.T) { releaseSettings(t, "postgres") }

func releaseSettings(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	require.NoError(t, db.AutoMigrate(&types.SystemSetting{}))
	repo := repository.NewSystemSettingRepository(db)
	svc := &systemSettingService{repo: repo, cache: map[string]*types.SystemSetting{}}
	ctx := context.Background()
	resolve := func() int {
		return types.ResolveWorkerPoolConcurrency(func(key, env string, fallback int) int {
			return int(svc.GetInt(ctx, key, env, int64(fallback)))
		}).Maintenance
	}
	t.Setenv("WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY", "")
	require.Equal(t, 4, resolve(), "actual resolver: default without DB or ENV")
	t.Setenv("WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY", "1")
	require.Equal(t, 1, resolve(), "actual resolver: environment over default")
	for _, tc := range []struct {
		value, modifier string
		want            int
	}{
		{"4", "", 1}, // untouched bootstrap row must not mask the environment
		{"4", "admin", 4},
		{"3", "", 3},
		{"1", "admin", 1},
		{`"invalid"`, "admin", 1},
	} {
		require.NoError(t, repo.Upsert(ctx, &types.SystemSetting{
			Key: "asynq.maintenance_concurrency", Value: types.JSON(tc.value),
			ValueType: "int", Category: "asynq", LastModifiedBy: tc.modifier,
		}))
		require.Equal(t, tc.want, resolve())
	}
}
