package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalRecoveryExactAccessMetadataPreservesStorageSelection(t *testing.T) {
	f := newMVPFixture(t)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}, &types.DataSource{}))
	require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources (
id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,knowledge_id text,state text)`).Error)
	require.NoError(t, f.db.Create(&types.DataSource{
		ID: "held-sibling", TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu_drive",
		LegacyAccessState: "revoked", Config: types.JSON(`{}`),
	}).Error)
	require.NoError(t, f.db.Exec(`UPDATE knowledge_bases SET
storage_backend_id='bound-storage',storage_provider_config='{"provider":"local","unexpected_secret":"secret-marker"}',
description='restricted-description',generated_profile='{"summary":"restricted-profile"}' WHERE id='kb'`).Error)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		Scope: types.PersonalExactReceiptScope, ScopeID: "exact", TenantID: 1,
		DedupKey: "exact", Op: "staging", EnqueuedAt: time.Now(),
		Payload: json.RawMessage(`{"knowledge_base_id":"kb"}`),
	}).Error)
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	types.EnableLegacyReadGuards(f.db)
	ctx := types.WithPrincipal(personalUser("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleViewer})
	svc := &knowledgeBaseService{personalRecovery: f.p}
	row, err := svc.GetKnowledgeBaseAccessMetadata(ctx, "kb")
	require.NoError(t, err)
	require.NotNil(t, row.StorageBackendID)
	require.Equal(t, "bound-storage", *row.StorageBackendID)
	require.NotNil(t, row.StorageProviderConfig)
	require.Equal(t, "local", row.StorageProviderConfig.Provider)
	data, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(data), "restricted-description")
	require.NotContains(t, string(data), "restricted-profile")
	require.NotContains(t, string(data), "secret-marker")
	var protected types.KnowledgeBase
	require.ErrorIs(t, f.db.WithContext(ctx).Where("id='kb'").First(&protected).Error,
		types.ErrLegacySourceRestricted)
	foreign := types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "B", Role: types.TenantRoleViewer})
	_, err = svc.GetKnowledgeBaseAccessMetadata(foreign, "kb")
	require.Error(t, err)
}
