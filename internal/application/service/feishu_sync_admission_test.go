package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestSteadyManualProtectedAndPausedSourcesHaveNoAdmissionSideEffects(t *testing.T) {
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false") // Source protection is independent of this test's transport.
	db := syncdb.Open(t, "sqlite")
	repo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs}
	ctx := context.Background()
	for _, item := range []struct{ state, status, reason string }{
		{"hold", "active", "sync_source_protected"},
		{"revoked", "active", "sync_source_protected"},
		{"unmanaged", "paused", "sync_source_paused"},
	} {
		ds := &types.DataSource{
			ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: uuid.NewString(), Name: "isolated-admission",
			Type: "feishu_drive", Status: item.status, LegacyAccessState: item.state,
			Config: types.JSON(`{"resource_ids":["synthetic"],"settings":{}}`),
		}
		require.NoError(t, repo.Create(ctx, ds))
		_, err := svc.manualSyncUsing(ctx, ds.ID, func(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
			t.Fatal("denied source reached enqueue")
			return nil, nil
		})
		var rejected *datasource.SyncAdmissionError
		require.True(t, errors.As(err, &rejected))
		require.Equal(t, item.reason, rejected.Reason)
		if item.reason == "sync_source_protected" {
			require.Equal(t, item.state, rejected.ProtectionState)
		}
		stored, err := logs.FindByDataSource(ctx, ds.ID, 20, 0)
		require.NoError(t, err)
		require.Empty(t, stored)
	}
}

func TestSteadyManualActiveWikiAndDriveUseApprovedQueue(t *testing.T) {
	for _, kind := range []string{"feishu", "feishu_drive"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			db := syncdb.Open(t, "sqlite")
			repo := repository.NewDataSourceRepository(db)
			logs := repository.NewSyncLogRepository(db)
			ds := &types.DataSource{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: uuid.NewString(), Name: "isolated-steady",
				Type: kind, Status: "active", LegacyAccessState: "unmanaged",
				Config: types.JSON(`{"resource_ids":["synthetic-root"],"settings":{}}`),
			}
			require.NoError(t, repo.Create(ctx, ds))
			facts := func(context.Context, string) (feishusynccontrol.RecoveryFacts, error) {
				fp, err := feishusafety.SourceConfigFingerprint(ds.ID, ds.KnowledgeBaseID, ds.Type,
					ds.LegacyAccessState, ds.TenantID, ds.Config)
				return feishusynccontrol.RecoveryFacts{
					SourceID: ds.ID, Eligible: true, SelectedResourceCount: 1,
					ConfigSHA256: fp, FactsSHA256: strings.Repeat("c", 64),
				}, err
			}
			authority := func(
				_ context.Context, id, actor string,
			) (feishusynccontrol.SourceManagementAuthority, error) {
				return feishusynccontrol.SourceManagementAuthority{
					SourceID: id, TenantID: ds.TenantID,
					KBID: ds.KnowledgeBaseID, SourceType: kind, SourceStatus: "active", AccessState: "unmanaged",
					ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
				}, nil
			}
			feishucontinuity.ActivateSteady(t, []string{ds.ID}, facts, authority)
			owned := miniredis.RunT(t)
			client := asynq.NewClient(asynq.RedisClientOpt{Addr: owned.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs}
			deliveries := 0
			enqueue := func(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
				deliveries++
				info, err := client.Enqueue(task)
				require.NoError(t, err)
				require.Equal(t, types.QueueApprovedSync, info.Queue)
				return info, err
			}
			log, err := svc.manualSyncUsing(ctx, ds.ID, enqueue)
			require.NoError(t, err)
			require.NotEmpty(t, log.ID)
			require.Equal(t, 1, deliveries)
			stored, err := logs.FindByDataSource(ctx, ds.ID, 20, 0)
			require.NoError(t, err)
			require.Len(t, stored, 1)
		})
	}
}
