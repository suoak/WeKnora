package service

import (
	"context"
	"encoding/json"
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

func TestSourceReconciliationServiceQueuesOneFullRunWithoutReleasingHold(t *testing.T) {
	for _, kind := range []string{"feishu", "feishu_drive"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.UserIDContextKey, "fixture-operator")
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "fixture-operator"})
			db := syncdb.Open(t, "sqlite")
			repo := repository.NewDataSourceRepository(db)
			logs := repository.NewSyncLogRepository(db)
			makeSource := func(state string) *types.DataSource {
				ds := &types.DataSource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: uuid.NewString(), Name: "synthetic-held",
					Type: kind, Status: "active", LegacyAccessState: state,
					Config: types.JSON(`{"resource_ids":["synthetic-root"],"settings":{}}`),
				}
				require.NoError(t, repo.Create(ctx, ds))
				return ds
			}
			anchor, held := makeSource("unmanaged"), makeSource("hold")
			facts := func(ctx context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
				ds, err := repo.FindByID(ctx, id)
				if err != nil {
					return feishusynccontrol.RecoveryFacts{}, err
				}
				fp, err := feishusafety.SourceConfigFingerprint(
					id, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.TenantID, ds.Config)
				return feishusynccontrol.RecoveryFacts{
					SourceID: id, ConfigSHA256: fp, FactsSHA256: strings.Repeat("c", 64),
					Eligible:               ds.LegacyAccessState == "unmanaged",
					ReconciliationEligible: ds.LegacyAccessState == "hold", SelectedResourceCount: 1,
				}, err
			}
			authority := func(
				ctx context.Context, id, actor string,
			) (feishusynccontrol.SourceManagementAuthority, error) {
				ds, err := repo.FindByID(ctx, id)
				if err != nil {
					return feishusynccontrol.SourceManagementAuthority{}, err
				}
				return feishusynccontrol.SourceManagementAuthority{
					SourceID: id, TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID, SourceType: kind,
					SourceStatus: ds.Status, AccessState: ds.LegacyAccessState,
					ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
				}, nil
			}
			p := feishucontinuity.ActivateSteadyReconciliation(
				t, []string{anchor.ID}, []string{held.ID}, facts, authority)
			redis := miniredis.RunT(t)
			client := asynq.NewClient(asynq.RedisClientOpt{Addr: redis.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs, taskEnqueuer: client}
			_, err := svc.ManualSync(ctx, held.ID)
			require.Error(t, err)
			log, err := svc.RunSourceReconciliation(ctx, held.ID, p.ID)
			require.NoError(t, err)
			require.NotEmpty(t, log.ID)
			inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr()})
			t.Cleanup(func() { require.NoError(t, inspector.Close()) })
			tasks, err := inspector.ListPendingTasks(types.QueueApprovedSync)
			require.NoError(t, err)
			require.Len(t, tasks, 1)
			var payload types.DataSourceSyncPayload
			require.NoError(t, json.Unmarshal(tasks[0].Payload, &payload))
			require.True(t, payload.ForceFull)
			require.Equal(t, "source_reconciliation", payload.Trigger)
			require.Equal(t, p.ID, payload.ReconciliationPolicyID)
			require.Equal(t, log.ID, payload.SyncLogID)
			current, err := repo.FindByID(ctx, held.ID)
			require.NoError(t, err)
			require.Equal(t, "hold", current.LegacyAccessState)
			_, err = svc.RunSourceReconciliation(ctx, held.ID, p.ID)
			require.Error(t, err)
			stored, err := logs.FindByDataSource(ctx, held.ID, 20, 0)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			require.False(t, stored[0].ExecutionClaimed, "admission is not an execution Claim")
			t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
			execution, claimed, err := logs.ClaimExecution(ctx, held.ID, log.ID, held.TenantID)
			require.NoError(t, err)
			require.True(t, claimed)
			workerContext := types.WithSyncExecution(ctx, execution)
			work, err := feishusynccontrol.RestoreSourceReconciliation(workerContext, held.ID, p.ID)
			require.NoError(t, err)
			partial := &types.SyncResult{Total: 2, Skipped: 1, Failed: 1}
			reconciles := func(check context.Context, full bool) bool {
				return types.LegacyFullReconciliation(fullFeishuReconciliationContext(check, held, full, partial))
			}
			require.True(t, reconciles(work, true))
			require.False(t, reconciles(ctx, true))
			readOnly := types.WithLegacyReconciliationRead(types.WithSyncExecution(ctx, execution))
			require.False(t, reconciles(readOnly, true))
			require.False(t, reconciles(work, false))
			partial.Deferred = 1
			require.False(t, reconciles(work, true))
		})
	}
}
