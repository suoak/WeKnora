package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func testQueuedSourceReconciliation(t *testing.T, identity *FeishuPersonalService,
	ctx context.Context, source, scenario string,
) {
	t.Helper()
	db := identity.db
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "A"})
	var ds types.DataSource
	require.NoError(t, db.First(&ds, "id=?", source).Error)
	if scenario == "queued_unsigned" {
		require.Error(t, identity.approveQueuedSourceReconciliation(ctx, &ds, uuid.NewString()))
		var count int64
		require.NoError(t, db.Model(&types.SyncLog{}).Count(&count).Error)
		require.Zero(t, count)
		return
	}
	if scenario != "queued_reconciliation" {
		run := types.SyncLog{
			ID: uuid.NewString(), DataSourceID: source, TenantID: ds.TenantID,
			Status: types.SyncLogStatusRunning, StartedAt: time.Now(),
		}
		switch scenario {
		case "queued_foreign_run":
			run.TenantID++
		case "queued_claimed_run":
			run.ExecutionClaimed = true
		case "queued_generation":
			run.ExecutionGeneration = 1
		case "queued_revoked_approval":
			require.NoError(t, db.Model(&types.FeishuSharingApproval{}).
				Where("reason=?", "NEW-publication-approval").Update("state", "revoked").Error)
		}
		require.NoError(t, db.Create(&run).Error)
		_, err := identity.approveSourceUpdates(ctx, source, "A", "synthetic-bound-run",
			time.Now().Add(time.Minute), run.ID, true)
		require.Error(t, err)
		var count int64
		require.NoError(t, db.Model(&types.SystemSetting{}).Where("key=?", feishuSyncPolicyPrefix+source).
			Count(&count).Error)
		require.Zero(t, count, "failed validation cannot enroll a publication grant")
		return
	}
	anchor := types.DataSource{
		ID: uuid.NewString(), TenantID: ds.TenantID, KnowledgeBaseID: ds.KnowledgeBaseID,
		Type: "feishu", Status: "active", LegacyAccessState: "unmanaged",
		Config: types.JSON(`{"resource_ids":["synthetic-root"],"settings":{}}`),
	}
	require.NoError(t, db.Create(&anchor).Error)
	repo := repository.NewDataSourceRepository(db)
	facts := func(check context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		row, err := repo.FindByID(check, id)
		if err != nil {
			return feishusynccontrol.RecoveryFacts{}, err
		}
		fp, err := feishusafety.SourceConfigFingerprint(
			id, row.KnowledgeBaseID, row.Type, row.LegacyAccessState, row.TenantID, row.Config)
		return feishusynccontrol.RecoveryFacts{
			SourceID: id, ConfigSHA256: fp, FactsSHA256: strings.Repeat("c", 64),
			Eligible: row.LegacyAccessState == "unmanaged", ReconciliationEligible: row.LegacyAccessState == "hold",
			SelectedResourceCount: 1,
		}, err
	}
	authority := func(check context.Context, id, actor string) (feishusynccontrol.SourceManagementAuthority, error) {
		row, err := repo.FindByID(check, id)
		if err != nil {
			return feishusynccontrol.SourceManagementAuthority{}, err
		}
		return feishusynccontrol.SourceManagementAuthority{
			SourceID: id, TenantID: row.TenantID, KBID: row.KnowledgeBaseID, SourceType: row.Type,
			SourceStatus: row.Status, AccessState: row.LegacyAccessState,
			ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
		}, nil
	}
	plan := feishucontinuity.ActivateSteadyReconciliationAs(
		t, "A", []string{anchor.ID}, []string{source}, facts, authority)
	r := miniredis.RunT(t)
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: r.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	logs := repository.NewSyncLogRepository(db)
	svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs, taskEnqueuer: client, feishuIdentity: identity}
	run, err := svc.RunSourceReconciliation(ctx, source, plan.ID)
	require.NoError(t, err)
	var setting types.SystemSetting
	require.NoError(t, db.First(&setting, "key=?", feishuSyncPolicyPrefix+source).Error)
	cipher, err := setting.AsString()
	require.NoError(t, err)
	var grant FeishuSyncPublicationPolicy
	require.NoError(t, openFeishu(cipher, &grant))
	require.Equal(t, run.ID, grant.ReconciliationLogID)
	require.Equal(t, "A", grant.Operator)
	require.Equal(t, "signed-source-reconciliation:"+plan.ID, grant.AuthorizationRef)
	var count int64
	require.NoError(t, db.Model(&types.SyncLog{}).Where("data_source_id=?", source).Count(&count).Error)
	require.EqualValues(t, 1, count, "publication consent must reuse the SQL admission run")
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: r.Addr()})
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	tasks, err := inspector.ListPendingTasks(types.QueueApprovedSync)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	var payload types.DataSourceSyncPayload
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &payload))
	require.Equal(t, run.ID, payload.SyncLogID)
	require.Equal(t, plan.ID, payload.ReconciliationPolicyID)
	require.False(t, run.ExecutionClaimed)
	var after types.DataSource
	require.NoError(t, db.First(&after, "id=?", source).Error)
	require.Equal(t, "hold", after.LegacyAccessState)
	require.Equal(t, string(ds.Config), string(after.Config))
	require.Equal(t, string(ds.LastSyncCursor), string(after.LastSyncCursor))
}
