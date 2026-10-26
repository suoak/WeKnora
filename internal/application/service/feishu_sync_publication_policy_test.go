package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresContinuingSourcePublicationPolicy(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.NoError(t, f.db.AutoMigrate(&types.DataSource{}))
	require.NoError(t, f.db.Exec("CREATE TABLE kb_shares(id text PRIMARY KEY,knowledge_base_id text,"+
		"organization_id text,source_tenant_id bigint,permission text,deleted_at timestamptz)").Error)
	_, _, credential, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	approval, err := f.s.ApproveSharing(f.ctx, "kb", credential.ConnectionID,
		"https://test.feishu.cn/file/real", "explicit first version", true)
	require.NoError(t, err)
	imported, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	ref := &types.FeishuSourceReference{
		Version: 1, OwnerUserID: "A", Kind: credential.Kind,
		ConnectionID: credential.ConnectionID, AppID: credential.AppID, IdentityVersion: credential.ConfigVersion,
	}
	config, err := (&types.DataSourceConfig{Settings: map[string]interface{}{
		"credential_reference": ref, "links": []string{"https://test.feishu.cn/file/real"},
	}}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: imported.ID, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: types.DataSourceStatusActive, Config: config,
	}
	require.NoError(t, f.db.Create(ds).Error)
	// PostgreSQL JSONB may normalize bytes; the policy binds persisted config.
	require.NoError(t, f.db.Where("id = ?", ds.ID).First(ds).Error)
	ctx := types.WithSyncExecution(f.ctx, types.SyncExecution{DataSourceID: ds.ID, TenantID: 1})
	resource := core.PersonalResource{Type: "file", Token: "real", Modified: "2"}
	check := func(ctx context.Context, selected *types.DataSource, identity *types.FeishuSourceReference,
		object core.PersonalResource,
	) error {
		return f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return checkSourcePublicationPolicy(tx, selected, identity, object)
		})
	}
	require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval, "absent by default")
	_, err = f.s.ApproveSourceUpdates(f.ctx, ds.ID, "operator", "explicit-policy-reference", time.Now().Add(time.Hour))
	require.Error(t, err, "ordinary import users cannot enroll an administrator policy")
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	policy, err := f.s.ApproveSourceUpdates(
		admin,
		ds.ID,
		"test-operator",
		"explicit-policy-reference",
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, approval.ID, policy.InitialApprovalID)
	require.NoError(t, check(ctx, ds, ref, resource), "new version uses the explicit continuing grant")
	f.revision.Store(2)
	bound, err := (&DataSourceService{feishuIdentity: f.s}).bindFeishuSource(ctx, ds)
	require.NoError(t, err)
	guard, ok := types.FeishuSourceGuardFromContext(bound)
	require.True(t, ok)
	require.NoError(t, f.db.WithContext(bound).Transaction(guard), "real commit guard accepts the scoped policy")
	var epoch types.FeishuPersonalGrantEpoch
	require.NoError(t, f.db.Where("user_id = ?", "A").First(&epoch).Error)
	priorEpoch := epoch.Epoch
	require.NoError(t, f.db.Model(&epoch).Update("epoch", "new-grant").Error)
	require.ErrorIs(
		t,
		check(ctx, ds, ref, resource),
		ErrFeishuSharingApproval,
		"reconnecting cannot revive a revoked grant",
	)
	require.NoError(t, f.db.Model(&epoch).Update("epoch", priorEpoch).Error)
	require.ErrorIs(t, check(f.ctx, ds, ref, resource), ErrFeishuSharingApproval, "quick import is not an approval")
	other := *ref
	other.OwnerUserID = "B"
	require.ErrorIs(t, check(ctx, ds, &other, resource), ErrFeishuSharingApproval)
	other = *ref
	other.Kind = types.FeishuApplicationGrant
	require.ErrorIs(t, check(ctx, ds, &other, resource), ErrFeishuSharingApproval)
	object := resource
	object.Token = "another-document"
	require.ErrorIs(t, check(ctx, ds, ref, object), ErrFeishuSharingApproval)
	object = resource
	object.Modified = ""
	require.ErrorIs(t, check(ctx, ds, ref, object), ErrFeishuSharingApproval)
	for _, status := range []string{types.DataSourceStatusPaused, types.DataSourceStatusDeleted} {
		require.NoError(t, f.db.Model(ds).Update("status", status).Error)
		require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval)
	}
	require.NoError(t, f.db.Model(ds).Update("status", types.DataSourceStatusActive).Error)
	for _, state := range []string{"hold", "revoked"} {
		require.NoError(t, f.db.Model(ds).Update("legacy_access_state", state).Error)
		require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval)
	}
	require.NoError(t, f.db.Model(ds).Update("legacy_access_state", "unmanaged").Error)
	require.NoError(t, f.db.Model(approval).Update("state", "revoked").Error)
	require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval)
	require.NoError(t, f.db.Model(approval).Update("state", "approved").Error)
	require.NoError(t, f.db.Exec("INSERT INTO kb_shares VALUES('new-share','kb','outside',1,'viewer',NULL)").Error)
	require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval, "sharing scope cannot expand")
	require.NoError(t, f.db.Exec("DELETE FROM kb_shares WHERE id='new-share'").Error)
	policy.ExpiresAt = time.Now().Add(-time.Minute)
	cipher, err := sealFeishu(policy)
	require.NoError(t, err)
	encoded, err := json.Marshal(cipher)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.SystemSetting{}).Where("key = ?", feishuSyncPolicyPrefix+ds.ID).
		Update("value", types.JSON(encoded)).Error)
	require.ErrorIs(t, check(ctx, ds, ref, resource), ErrFeishuSharingApproval)
}
