package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSourceReconciliationPreparesExistingPersonalGrantForTheSameRun(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.NoError(t, f.db.AutoMigrate(&types.DataSource{}))
	require.NoError(t, f.db.Exec("CREATE TABLE kb_shares(id text PRIMARY KEY,knowledge_base_id text,"+
		"organization_id text,source_tenant_id bigint,permission text,deleted_at timestamptz)").Error)
	_, _, credential, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	approval, err := f.s.ApproveSharing(f.ctx, "kb", credential.ConnectionID,
		"https://test.feishu.cn/file/real", "explicit owner consent", true)
	require.NoError(t, err)
	imported, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Where("id=?", approval.ID).
		Update("expires_at", time.Now().Add(-time.Hour)).Error)
	ref := &types.FeishuSourceReference{
		Version: 1, OwnerUserID: "A", Kind: credential.Kind, ConnectionID: credential.ConnectionID,
		AppID: credential.AppID, IdentityVersion: credential.ConfigVersion,
	}
	config, err := (&types.DataSourceConfig{ResourceIDs: []string{"reference"}, Settings: map[string]any{
		"credential_reference": ref, "links": []string{"https://test.feishu.cn/file/real"},
	}}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: imported.ID, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: "error", LegacyAccessState: "hold", Config: config,
	}
	require.NoError(t, f.db.Create(ds).Error)
	require.NoError(t, f.db.First(ds, "id=?", ds.ID).Error)
	anchor := &types.DataSource{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: "active", LegacyAccessState: "unmanaged", Config: types.JSON(`{"resource_ids":["anchor"]}`),
	}
	require.NoError(t, f.db.Create(anchor).Error)
	require.NoError(t, f.db.Exec("INSERT INTO users(id,is_active,is_system_admin) VALUES(?,true,true)",
		"fixture-operator").Error)
	ctx := context.WithValue(f.ctx, types.UserIDContextKey, "fixture-operator")
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "fixture-operator"})
	repo, logs := repository.NewDataSourceRepository(f.db), repository.NewSyncLogRepository(f.db)
	redis := miniredis.RunT(t)
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: redis.Addr()})
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs, taskEnqueuer: queue, feishuIdentity: f.s}
	prepared, err := svc.prepareSignedReferenceReconciliation(ctx, ds)
	require.NoError(t, err)
	require.Nil(t, prepared, "ordinary sync cannot mint a publication grant")
	_, err = svc.ManualSync(ctx, ds.ID)
	require.Error(t, err, "an administrator without a signed purpose cannot impersonate the source owner")
	facts := func(work context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		row, err := repo.FindByID(work, id)
		if err != nil {
			return feishusynccontrol.RecoveryFacts{}, err
		}
		fp, err := feishusafety.SourceConfigFingerprint(id, row.KnowledgeBaseID, row.Type,
			row.LegacyAccessState, row.TenantID, row.Config)
		return feishusynccontrol.RecoveryFacts{
			SourceID: id, ConfigSHA256: fp,
			FactsSHA256: strings.Repeat("c", 64), Eligible: row.LegacyAccessState == "unmanaged",
			ReconciliationEligible: row.LegacyAccessState == "hold", SelectedResourceCount: 1,
		}, err
	}
	authority := func(work context.Context, id, actor string) (feishusynccontrol.SourceManagementAuthority, error) {
		row, err := repo.FindByID(work, id)
		if err != nil {
			return feishusynccontrol.SourceManagementAuthority{}, err
		}
		return feishusynccontrol.SourceManagementAuthority{
			SourceID: id, TenantID: row.TenantID,
			KBID: row.KnowledgeBaseID, SourceType: row.Type, SourceStatus: row.Status,
			AccessState: row.LegacyAccessState, ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
		}, nil
	}
	plan := feishucontinuity.ActivateSteadyReconciliation(t, []string{anchor.ID}, []string{ds.ID}, facts, authority)
	run, err := svc.RunSourceReconciliation(ctx, ds.ID, plan.ID)
	require.NoError(t, err)
	stored, err := logs.FindByDataSource(ctx, ds.ID, 20, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1, "controller and SQL grant share one durable run")
	require.Equal(t, run.ID, stored[0].ID)
	var grant FeishuSyncPublicationPolicy
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		return readSourceSetting(tx, feishuSyncPolicyPrefix+ds.ID, &grant)
	}))
	require.Equal(t, run.ID, grant.ReconciliationLogID)
	require.Equal(t, approval.ID, grant.InitialApprovalID)
	require.Equal(t, time.UnixMilli(plan.AuthorizationUntilMS), grant.ExpiresAt)
	require.True(t, grant.ReconciliationExpiresAt.After(time.Now().Add(30*time.Minute)),
		"the explicitly signed read window replaces the legacy fifteen-minute cap")
	var source types.DataSource
	require.NoError(t, f.db.First(&source, "id=?", ds.ID).Error)
	require.Equal(t, "hold", source.LegacyAccessState)
	executor := logs.(interfaces.SyncExecutionRepository)
	execution, claimed, err := executor.ClaimExecution(ctx, ds.ID, run.ID, 1)
	require.NoError(t, err)
	require.True(t, claimed)
	work := types.WithSyncExecution(ctx, execution)
	require.NoError(t, f.s.checkHeldSourceReconciliation(work, &source, ref),
		"the existing personal-source publication fence accepts the same signed run")
	stale := execution
	stale.Generation++
	require.Error(t, f.s.checkHeldSourceReconciliation(types.WithSyncExecution(ctx, stale), &source, ref))
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Where("id=?", approval.ID).
		Update("state", "revoked").Error)
	require.Error(t, f.s.checkHeldSourceReconciliation(work, &source, ref),
		"a signed recovery never overrides withdrawn owner publication consent")
}
