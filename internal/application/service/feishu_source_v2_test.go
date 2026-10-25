package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresSourceReadAndCommitRevocation(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	cfg := &types.DataSourceConfig{Settings: map[string]interface{}{
		"credential_reference": types.FeishuSourceReference{Version: 1, OwnerUserID: "A", Kind: ref.Kind, ConnectionID: ref.ConnectionID, AppID: ref.AppID, IdentityVersion: ref.ConfigVersion},
		"links":                []string{"https://test.feishu.cn/file/real"},
	}}
	blob, err := cfg.ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeFeishu, Config: blob}
	s := &DataSourceService{feishuIdentity: f.s}
	_, err = s.bindFeishuSource(f.userContext("B"), ds)
	require.Error(t, err)
	bound, err := s.bindFeishuSource(f.ctx, ds)
	require.NoError(t, err)
	require.NoError(t, types.CheckFeishuSourceRead(bound))
	commit, ok := types.FeishuSourceGuardFromContext(bound)
	require.True(t, ok)
	require.NoError(t, f.db.WithContext(bound).Transaction(commit))
	f.revision.Store(2)
	require.Error(t, types.CheckFeishuSourceRead(bound), "a changed revision must never publish a stale candidate")
	f.revision.Store(1)
	require.NoError(t, f.s.Disconnect(f.ctx))
	require.Error(t, types.CheckFeishuSourceRead(bound))
	require.Error(t, f.db.WithContext(bound).Transaction(commit), "the local commit fence also rejects a deleted grant")
}

// This tests the transactional handoff primitive; indexing verification remains
// the responsibility of EnableImportSource and the native engine release gate.
func TestFeishuV2PostgresSourceHandoffIsAtomicAndFencesOldWorker(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var before types.FeishuPersonalResource
	require.NoError(t, f.db.Where("import_id = ?", m.ID).First(&before).Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Where("id = ?", m.ID).Update("status", "synced").Error)
	lease := time.Now().Add(time.Minute)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id = ?", m.AttemptID).Update("lease_until", lease).Error)
	called := false
	callback := func(tx *gorm.DB, current *types.FeishuPersonalImport) error {
		called = true
		require.Equal(t, m.LogicalKnowledgeID, current.KnowledgeID)
		return nil
	}
	err = f.p.repo.HandoffSource(f.ctx, m.ID, "A", callback)
	require.ErrorIs(t, err, repository.ErrPersonalBusy)
	require.False(t, called)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id = ?", m.AttemptID).Update("lease_until", nil).Error)
	fault := errors.New("handoff rollback")
	err = f.p.repo.HandoffSource(f.ctx, m.ID, "A", func(tx *gorm.DB, _ *types.FeishuPersonalImport) error {
		require.NoError(t, tx.Model(&types.Knowledge{}).Where("id = ?", m.LogicalKnowledgeID).Update("title", "must roll back").Error)
		return fault
	})
	require.ErrorIs(t, err, fault)
	var knowledge types.Knowledge
	require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&knowledge).Error)
	require.NotEqual(t, "must roll back", knowledge.Title)
	require.NoError(t, f.p.repo.HandoffSource(f.ctx, m.ID, "A", callback))
	var after types.FeishuPersonalResource
	var attempt types.FeishuPersonalAttempt
	require.NoError(t, f.db.Where("import_id = ?", m.ID).First(&after).Error)
	require.Equal(t, before.Generation+1, after.Generation)
	require.Equal(t, before.LogicalKnowledgeID, after.LogicalKnowledgeID)
	require.NoError(t, f.db.Where("id = ?", m.AttemptID).First(&attempt).Error)
	require.Equal(t, "obsolete", attempt.Status)
	called = false
	require.NoError(t, f.p.repo.HandoffSource(context.Background(), m.ID, "A", callback))
	require.False(t, called, "an idempotent retry must not repeat metadata mutation")
	err = f.p.repo.HandoffSource(f.userContext("B"), m.ID, "B", callback)
	require.Error(t, err)
}
