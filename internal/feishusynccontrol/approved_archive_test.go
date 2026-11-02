package feishusynccontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func TestSourceReconciliationSignedTerminalPendingRetainsDeliveryAndSharedHistory(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	client := asynq.NewClientFromRedisClient(f.r)
	payload := types.DataSourceSyncPayload{DataSourceID: f.ids[0], TenantID: 1, SyncLogID: uuid.NewString()}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	info, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body), asynq.Queue(ApprovedSyncQueue))
	require.NoError(t, err)
	key := "asynq:{" + ApprovedSyncQueue + "}:t:" + info.ID
	original := f.r.HGetAll(ctx, key).Val()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	_, err = f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.Error(t, err, "pending is not SQL terminal proof")
	terminal := true
	f.m.ConfigureArchivedTerminal(func(_ context.Context, rows []types.DataSourceSyncPayload) (bool, error) {
		return terminal && len(rows) == 1 && rows[0].DataSourceID == payload.DataSourceID &&
			rows[0].SyncLogID == payload.SyncLogID && rows[0].TenantID == payload.TenantID, nil
	})
	p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.NoError(t, err)
	require.Equal(t, 1, p.TerminalPendingCount)
	require.Len(t, p.TerminalPendingSHA256, 64)
	require.Zero(t, p.TerminalArchivedCount)
	terminal = false
	require.Error(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"), "terminal state must be rechecked")
	terminal = true
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.Equal(t, original, f.r.HGetAll(ctx, key).Val(), "admission must not edit or delete the delivery")
	require.Equal(t, []string{info.ID}, f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val())
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.digest, after.digest, "the six historical shared deliveries must remain byte-identical")
}

func TestSourceReconciliationTerminalPendingSnapshotChangesRejectAdmission(t *testing.T) {
	for _, scenario := range []string{"message_changed", "new_pending", "scheduled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSteadyFixture(t)
			ctx := context.Background()
			client := asynq.NewClientFromRedisClient(f.r)
			body, err := json.Marshal(types.DataSourceSyncPayload{
				DataSourceID: f.ids[0], TenantID: 1, SyncLogID: uuid.NewString(),
			})
			require.NoError(t, err)
			info, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body), asynq.Queue(ApprovedSyncQueue))
			require.NoError(t, err)
			f.m.ConfigureArchivedTerminal(func(context.Context, []types.DataSourceSyncPayload) (bool, error) {
				return true, nil
			})
			p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
			require.NoError(t, err)
			if scenario == "message_changed" {
				require.NoError(t, f.r.HSet(ctx, "asynq:{approved_sync}:t:"+info.ID, "msg", "invalid-wire").Err())
			} else {
				opts := []asynq.Option{asynq.Queue(ApprovedSyncQueue)}
				if scenario == "scheduled" {
					opts = append(opts, asynq.ProcessIn(time.Hour))
				}
				_, err = client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body), opts...)
				require.NoError(t, err)
			}
			require.Error(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
		})
	}
}
