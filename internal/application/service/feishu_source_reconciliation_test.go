package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresApprovedHeldSourceReconciliation(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.NoError(t, f.db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
	require.NoError(t, f.db.Exec("CREATE TABLE kb_shares(id text PRIMARY KEY,knowledge_base_id text,"+
		"organization_id text,source_tenant_id bigint,permission text,deleted_at timestamptz)").Error)
	_, _, credential, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	_, err = f.s.ApproveSharing(f.ctx, "kb", credential.ConnectionID,
		"https://test.feishu.cn/file/real", "explicit original publication", true)
	require.NoError(t, err)
	imported, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	ref := &types.FeishuSourceReference{
		Version: 1, OwnerUserID: "A", Kind: credential.Kind, ConnectionID: credential.ConnectionID,
		AppID: credential.AppID, IdentityVersion: credential.ConfigVersion,
	}
	config, err := (&types.DataSourceConfig{Settings: map[string]interface{}{
		"credential_reference": ref, "links": []string{"https://test.feishu.cn/file/real"},
	}}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: imported.ID, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: types.DataSourceStatusError, Config: config, LegacyAccessState: "hold",
		LastSyncCursor: types.JSON(`{"accepted":"original"}`),
	}
	require.NoError(t, f.db.Create(ds).Error)
	require.NoError(t, f.db.First(ds, "id = ?", ds.ID).Error)
	beforeCursor, beforeConfig := string(ds.LastSyncCursor), string(ds.Config)
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	_, err = f.s.ApproveSourceReconciliation(
		f.ctx,
		ds.ID,
		"operator",
		"new-explicit-approval",
		time.Now().Add(time.Hour),
	)
	require.Error(t, err, "ordinary import cannot approve hold recovery")
	_, err = f.s.ApproveSourceUpdates(admin, ds.ID, "operator", "ordinary-policy", time.Now().Add(time.Hour))
	require.Error(t, err, "continuing publication does not restore hold")
	policy, err := f.s.ApproveSourceReconciliation(admin, ds.ID, "operator", "new-explicit-approval",
		time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, policy.ReconciliationLogID)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), policy.ReconciliationExpiresAt, time.Second)
	require.NoError(t, f.db.First(ds, "id = ?", ds.ID).Error)
	require.Equal(t, "hold", ds.LegacyAccessState)
	require.Equal(t, beforeCursor, string(ds.LastSyncCursor))
	require.Equal(t, beforeConfig, string(ds.Config))
	check := func(log string, generation int64) error {
		ctx := types.WithSyncExecution(f.ctx, types.SyncExecution{
			DataSourceID: ds.ID, TenantID: 1, SyncLogID: log, Generation: generation,
		})
		return f.s.checkHeldSourceReconciliation(ctx, ds, ref)
	}
	require.Error(t, check(policy.ReconciliationLogID, 0), "unclaimed run has no authority")
	require.Error(t, check("another-run", 1))
	require.NoError(t, f.db.Model(&types.SyncLog{}).Where("id = ?", policy.ReconciliationLogID).
		Updates(map[string]any{"execution_generation": 1, "execution_claimed": true}).Error)
	require.NoError(t, check(policy.ReconciliationLogID, 1))
	require.Error(t, check(policy.ReconciliationLogID, 2), "stale generation rejected")
	svc := &DataSourceService{feishuIdentity: f.s}
	var copyDenied atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
		if r.URL.Path == "/open-apis/drive/v1/metas/batch_query" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"metas": []any{map[string]any{
					"doc_type": "file", "doc_token": "real", "title": "test.pdf", "latest_modify_time": "1",
				}},
			}})
			return
		}
		if r.URL.Path == "/open-apis/drive/v1/permissions/real/members/auth" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"auth_result": !copyDenied.Load() || r.URL.Query().Get("action") != "copy",
			}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client {
		cfg.BaseURL = server.URL
		return core.NewClient(cfg)
	}
	_, err = svc.bindFeishuSource(f.ctx, ds)
	require.Error(t, err, "owner session cannot use the administrative grant")
	ctx := types.WithSyncExecution(f.ctx, types.SyncExecution{
		DataSourceID: ds.ID, TenantID: 1, SyncLogID: policy.ReconciliationLogID, Generation: 1,
	})
	bound, err := svc.bindFeishuSource(ctx, ds)
	require.NoError(t, err)
	guard, ok := types.FeishuSourceGuardFromContext(bound)
	require.True(t, ok)
	require.NoError(t, types.CheckFeishuSourceRead(bound))
	copyDenied.Store(true)
	require.Error(t, types.CheckFeishuSourceRead(bound), "administrative recovery cannot override live copy denial")
	copyDenied.Store(false)
	require.NoError(t, f.db.WithContext(bound).Transaction(guard))
	for _, state := range []string{"revoked"} {
		require.NoError(t, f.db.Model(ds).Update("legacy_access_state", state).Error)
		require.Error(t, check(policy.ReconciliationLogID, 1))
	}
	require.NoError(t, f.db.Model(ds).Update("legacy_access_state", "hold").Error)
	require.NoError(t, f.db.Model(ds).Update("status", types.DataSourceStatusPaused).Error)
	require.Error(t, check(policy.ReconciliationLogID, 1))
	require.NoError(t, f.db.Model(ds).Update("status", types.DataSourceStatusError).Error)
	require.NoError(t, f.db.Model(&types.SyncLog{}).Where("id = ?", policy.ReconciliationLogID).
		Update("status", types.SyncLogStatusSuccess).Error)
	require.Error(t, check(policy.ReconciliationLogID, 1), "terminal run cannot revive future hold")
	require.NoError(t, f.db.Model(&types.SyncLog{}).Where("id = ?", policy.ReconciliationLogID).
		Update("status", types.SyncLogStatusRunning).Error)
	policy.ReconciliationExpiresAt = time.Now().Add(-time.Second)
	cipher, err := sealFeishu(policy)
	require.NoError(t, err)
	value, err := json.Marshal(cipher)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.SystemSetting{}).Where("key = ?", feishuSyncPolicyPrefix+ds.ID).
		Update("value", types.JSON(value)).Error)
	require.Error(t, check(policy.ReconciliationLogID, 1))
	require.Error(t, f.db.WithContext(bound).Transaction(guard), "expiry also closes the bound publication guard")
	require.Error(t, types.CheckFeishuSourceRead(bound), "expiry also closes further reads")
	var stored types.DataSource
	require.NoError(t, f.db.First(&stored, "id = ?", ds.ID).Error)
	require.Equal(t, "hold", stored.LegacyAccessState, "checking a grant never clears protection")
	require.Equal(t, beforeCursor, string(stored.LastSyncCursor))
	var original types.FeishuPersonalImport
	require.NoError(t, f.db.First(&original, "id = ?", imported.ID).Error)
	require.Equal(t, imported.KnowledgeID, original.KnowledgeID)
	// The continuing policy cannot be used as a new import's approval.
	require.ErrorIs(t, f.db.WithContext(f.ctx).Transaction(func(tx *gorm.DB) error {
		return checkSourcePublicationPolicy(
			tx,
			&stored,
			ref,
			core.PersonalResource{Type: "file", Token: "real", Modified: "2"},
		)
	}), ErrFeishuSharingApproval)
}
