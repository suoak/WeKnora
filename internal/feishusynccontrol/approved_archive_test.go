package feishusynccontrol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func archiveFixtureTask(t *testing.T, f *steadyFixture) (string, types.DataSourceSyncPayload) {
	t.Helper()
	client := asynq.NewClientFromRedisClient(f.r)
	payload := types.DataSourceSyncPayload{DataSourceID: f.ids[0], TenantID: 1, SyncLogID: uuid.NewString()}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	info, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body), asynq.Queue(ApprovedSyncQueue))
	require.NoError(t, err)
	inspector := asynq.NewInspectorFromRedisClient(f.r)
	require.NoError(t, inspector.ArchiveTask(ApprovedSyncQueue, info.ID))
	return info.ID, payload
}

func TestSourceReconciliationPolicyRetainsProvenTerminalArchives(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	id, payload := archiveFixtureTask(t, f)
	key := "asynq:{" + ApprovedSyncQueue + "}:t:" + id
	original := f.r.HGetAll(ctx, key).Val()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	_, err = f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.Error(t, err, "an archive label is not SQL terminal proof")
	f.m.ConfigureArchivedTerminal(func(_ context.Context, rows []types.DataSourceSyncPayload) (bool, error) {
		return len(rows) == 1 && rows[0].DataSourceID == payload.DataSourceID &&
			rows[0].SyncLogID == payload.SyncLogID && rows[0].TenantID == payload.TenantID, nil
	})
	p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.NoError(t, err)
	require.Equal(t, 1, p.TerminalArchivedCount)
	require.Len(t, p.TerminalArchiveSHA256, 64)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.Equal(t, original, f.r.HGetAll(ctx, key).Val())
	require.EqualValues(t, 1, f.r.ZCard(ctx, "asynq:{"+ApprovedSyncQueue+"}:archived").Val())
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.digest, after.digest)
}

func TestSourceReconciliationArchiveChangeAndUnknownExecutionRefuseReplacement(t *testing.T) {
	for _, scenario := range []string{"unknown_execution", "message_changed", "replayed_after_plan"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSteadyFixture(t)
			ctx := context.Background()
			id, _ := archiveFixtureTask(t, f)
			terminal := scenario != "unknown_execution"
			f.m.ConfigureArchivedTerminal(func(context.Context, []types.DataSourceSyncPayload) (bool, error) {
				return terminal, nil
			})
			p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
			if !terminal {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if scenario == "message_changed" {
				key := "asynq:{" + ApprovedSyncQueue + "}:t:" + id
				require.NoError(t, f.r.HSet(ctx, key, "msg", "invalid-task-wire").Err())
			} else {
				inspector := asynq.NewInspectorFromRedisClient(f.r)
				require.NoError(t, inspector.RunTask(ApprovedSyncQueue, id))
			}
			require.Error(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
		})
	}
}
