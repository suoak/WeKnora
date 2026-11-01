package datasource

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSteadySchedulerNaturalCronAcrossThreeSpaces(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDataSourceRepo()
	ids := []string{}
	for i := 0; i < 4; i++ {
		kind := []string{types.ConnectorTypeFeishu, types.ConnectorTypeFeishuDrive}[i%2]
		config, err := (&types.DataSourceConfig{Type: kind, ResourceIDs: []string{"synthetic-root"}}).ToJSON()
		require.NoError(t, err)
		ds := &types.DataSource{
			ID: uuid.NewString(), TenantID: uint64(i%3 + 1), KnowledgeBaseID: uuid.NewString(),
			Type: kind, Status: types.DataSourceStatusActive, LegacyAccessState: "unmanaged",
			Config: config, SyncSchedule: "* * * * * *",
		}
		require.NoError(t, repo.Create(ctx, ds))
		ids = append(ids, ds.ID)
	}
	facts := func(ctx context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		ds, err := repo.FindByID(ctx, id)
		if err != nil {
			return feishusynccontrol.RecoveryFacts{}, err
		}
		fp, err := feishusafety.SourceConfigFingerprint(ds.ID, ds.KnowledgeBaseID, ds.Type,
			ds.LegacyAccessState, ds.TenantID, ds.Config)
		return feishusynccontrol.RecoveryFacts{
			SourceID: id, Eligible: true, SelectedResourceCount: 1,
			ConfigSHA256: fp, FactsSHA256: strings.Repeat("c", 64),
		}, err
	}
	authority := func(ctx context.Context, id, actor string) (feishusynccontrol.SourceManagementAuthority, error) {
		ds, err := repo.FindByID(ctx, id)
		if err != nil {
			return feishusynccontrol.SourceManagementAuthority{}, err
		}
		return feishusynccontrol.SourceManagementAuthority{
			SourceID: id, TenantID: ds.TenantID,
			KBID: ds.KnowledgeBaseID, SourceType: ds.Type, SourceStatus: "active", AccessState: "unmanaged",
			ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
		}, nil
	}
	feishucontinuity.ActivateSteady(t, ids, facts, authority)
	queue := &fakeTaskEnqueuer{}
	scheduler := NewScheduler(repo, newFakeSyncLogRepo(), queue)
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()
	require.Eventually(t, func() bool { return queue.count.Load() == 4 }, 4*time.Second, 20*time.Millisecond)
	require.Equal(t, types.QueueApprovedSync, queue.lastQueue.Load())
	// No manual triggerSync call: all four deliveries came from natural cron.
}
