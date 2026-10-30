package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
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

func TestApprovedSourcesManualAdmissionAndRevocation(t *testing.T) {
	ctx := context.Background()
	db := syncdb.Open(t, "sqlite")
	repo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	config, err := (&types.DataSourceConfig{
		Type: types.ConnectorTypeFeishuDrive, ResourceIDs: []string{"synthetic-folder"},
	}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Name: "synthetic-drive",
		Type: types.ConnectorTypeFeishuDrive, Status: types.DataSourceStatusActive,
		LegacyAccessState: "unmanaged", Config: config,
	}
	require.NoError(t, repo.Create(ctx, ds))
	revoked := false
	feishucontinuity.Activate(t, ds.ID, func(ctx context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		current, e := repo.FindByID(ctx, id)
		if e != nil {
			return feishusynccontrol.RecoveryFacts{}, e
		}
		fp, e := feishusafety.SourceConfigFingerprint(current.ID, current.KnowledgeBaseID, current.Type,
			current.LegacyAccessState, current.TenantID, current.Config)
		return feishusynccontrol.RecoveryFacts{
			SourceID: id, ConfigSHA256: fp, FactsSHA256: strings.Repeat("c", 64),
			Eligible: !revoked, SelectedResourceCount: 1,
		}, e
	})
	svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs}
	queueRedis := miniredis.RunT(t)
	queueClient := asynq.NewClient(asynq.RedisClientOpt{Addr: queueRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, queueClient.Close()) })
	called := 0
	log, err := svc.manualSyncUsing(ctx, ds.ID, func(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
		called++
		info, enqueueErr := queueClient.Enqueue(task)
		require.NoError(t, enqueueErr)
		require.Equal(t, types.QueueApprovedSync, info.Queue)
		return info, nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, log.ID)
	require.Equal(t, 1, called)
	revoked = true
	_, err = svc.manualSyncUsing(ctx, ds.ID, func(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
		t.Fatal("revoked authority must not reach enqueue")
		return nil, nil
	})
	require.Error(t, err)
	stored, err := logs.FindByDataSource(ctx, ds.ID, 20, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1, "denial must precede a new SyncLog")
}
