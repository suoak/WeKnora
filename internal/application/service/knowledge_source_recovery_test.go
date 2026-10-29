package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceRestrictionPersonalReconcilePreservesIntent(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	imported := f.prepare("https://team.feishu.cn/file/real")
	_, resource, before, err := f.p.repo.Load(f.ctx, imported.AttemptID)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("CREATE TABLE feishu_legacy_resources(knowledge_base_id text,state text)").Error)
	require.NoError(
		t,
		f.db.Create(
			&types.DataSource{
				ID:                "held",
				TenantID:          1,
				KnowledgeBaseID:   "kb",
				Name:              "held fixture",
				Type:              "feishu_drive",
				Status:            "error",
				LegacyAccessState: "hold",
				Config:            types.JSON(`{}`),
			},
		).Error,
	)
	types.EnableLegacyReadGuards(f.db)
	downloads := f.downloads.Load()
	require.NoError(t, f.p.Reconcile(f.ctx))
	_, afterResource, after, err := f.p.repo.Load(f.ctx, imported.AttemptID)
	require.NoError(t, err)
	require.Equal(t, resource.Generation, afterResource.Generation)
	require.Equal(t, resource.CurrentAttemptID, afterResource.CurrentAttemptID)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.OwnerNonce, after.OwnerNonce)
	require.Equal(t, before.Fence, after.Fence)
	require.Equal(t, downloads, f.downloads.Load(), "held admission must not fetch more cloud bytes")
}

func TestSourceRestrictionMissingKBRetainsRecoveryCleanup(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	imported := f.prepare("https://team.feishu.cn/file/real")
	require.NoError(t, f.db.Exec("CREATE TABLE feishu_legacy_resources(knowledge_base_id text,state text)").Error)
	require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET deleted_at=now() WHERE id='kb'").Error)
	types.EnableLegacyReadGuards(f.db)
	require.NoError(t, f.p.Reconcile(f.ctx))
	_, _, after, err := f.p.repo.Load(f.ctx, imported.AttemptID)
	require.NoError(t, err)
	require.Equal(
		t,
		"needs_attention",
		after.Status,
		"missing resources retain existing invalidation instead of indefinitely pending admission",
	)
	require.Equal(t, "authorization_or_resource_missing", after.LastErrorCategory)
}
