package datasource

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApprovedSourcesSchedulerUsesSameAdmission(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDataSourceRepo()
	config, err := (&types.DataSourceConfig{
		Type: types.ConnectorTypeFeishuDrive, ResourceIDs: []string{"synthetic-folder"},
	}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb",
		Type: types.ConnectorTypeFeishuDrive, Status: types.DataSourceStatusActive, LegacyAccessState: "unmanaged",
		Config: config, SyncSchedule: "0 */5 * * * *",
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
	q := &fakeTaskEnqueuer{}
	logs := newFakeSyncLogRepo()
	scheduler := NewScheduler(repo, logs, q)
	scheduler.triggerSync(ds.ID, ds.TenantID)
	require.EqualValues(t, 1, q.count.Load())
	require.Equal(t, types.QueueApprovedSync, q.lastQueue.Load())
	revoked = true
	scheduler.triggerSync(ds.ID, ds.TenantID)
	require.EqualValues(t, 1, q.count.Load(), "revoked authority must not enqueue")
	other := *ds
	other.ID = uuid.NewString()
	require.NoError(t, repo.Create(ctx, &other))
	scheduler.triggerSync(other.ID, other.TenantID)
	require.EqualValues(t, 1, q.count.Load(), "unapproved source must not enqueue")
}
