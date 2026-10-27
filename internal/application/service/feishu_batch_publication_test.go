package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresInlinePublicationUsesOrdinaryKBWriter(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "B").Update("role", "contributor").Error)
	ctx := f.userContext("B") // Deliberately stale admin claim: live membership is authoritative.
	view, err := f.s.QuickImport(ctx, "kb", "writer-direct", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	require.Nil(t, view.Batch.PublicationConsentAt)
	replay, err := f.s.QuickImport(ctx, "kb", "writer-direct", []string{"https://test.feishu.cn/file/real"}, true)
	require.NoError(t, err)
	require.Equal(t, view.Batch.ID, replay.Batch.ID, "legacy confirmation flags are not separate admissions")
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", current.Items[0].Status)
	require.Nil(t, current.Items[0].SharingApprovalID)
	var approvals int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&approvals).Error)
	require.Zero(t, approvals, "an import click must not manufacture a separate approval")
	var imported types.FeishuPersonalImport
	require.NoError(t, f.db.First(&imported, "id = ?", current.Items[0].ImportID).Error)
	var state types.FeishuPersonalResource
	require.NoError(t, f.db.First(&state, "import_id = ?", imported.ID).Error)
	_, _, attempt, err := f.p.repo.Load(ctx, state.CurrentAttemptID)
	require.NoError(t, err)
	// Both local worker authorization and the transaction at publication accept a
	// non-creator contributor. Viewer demotion and OAuth revocation still fence it.
	_, _, err = f.p.authorizeLocal(ctx, &imported, attempt)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "B").Update("role", "viewer").Error)
	_, _, err = f.p.authorizeLocal(ctx, &imported, attempt)
	require.ErrorIs(t, err, access.ErrForbidden)
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "B").Update("role", "contributor").Error)
	require.NoError(t, f.s.Disconnect(ctx))
	_, err = f.p.authorize(ctx, &imported, attempt)
	require.Error(t, err)
}

func TestFeishuV2PostgresInlinePublicationFencesLeaseAndLiveAuthority(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(f.ctx, "kb", "writer-fence", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	item := view.Items[0]
	item.Status, item.LeaseOwner = "processing", "worker-a"
	expired := time.Now().Add(-time.Minute)
	item.LeaseUntil = &expired
	require.NoError(t, f.db.Save(&item).Error)
	require.ErrorIs(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item), gorm.ErrRecordNotFound)
	future := time.Now().Add(time.Minute)
	item.LeaseUntil = &future
	require.NoError(t, f.db.Save(&item).Error)
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").Update("role", "viewer").Error)
	require.ErrorIs(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item), access.ErrForbidden)
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").Update("role", "contributor").Error)
	old := time.Now().Add(-25 * time.Hour)
	view.Batch.PublicationConsentAt = &old
	require.NoError(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item), "obsolete sharing confirmation expiry does not block a lawful KB write")
}

func TestFeishuV2PostgresInlineContinuePreservesOriginalBatch(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(f.ctx, "kb", "historical-blocked", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, f.db.Model(&view.Batch).Update("publication_consent_at", old).Error)
	require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).Updates(map[string]any{"status": "blocked", "error_category": "sharing_approval_required"}).Error)
	require.Error(t, f.s.ContinueBatch(f.userContext("B"), view.Batch.ID, false))
	require.NoError(t, f.s.ContinueBatch(f.ctx, view.Batch.ID, false))
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", current.Items[0].Status)
	require.Equal(t, view.Items[0].ID, current.Items[0].ID)
	require.NoError(t, f.s.RetryBatch(f.ctx, view.Batch.ID))
	again, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, current.Items[0].ImportID, again.Items[0].ImportID)
	var approvals, imports int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&approvals).Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.Zero(t, approvals)
	require.EqualValues(t, 1, imports)
}

func TestFeishuV2PostgresInlinePublicationKeepsPrivateIsolation(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	require.ErrorIs(t, f.s.requireImportWriter(f.db.WithContext(f.userContext("B")), "B", "kb", 1), access.ErrForbidden)
	require.NoError(t, f.s.requireImportWriter(f.db.WithContext(f.ctx), "A", "kb", 1))
	require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").Update("status", "suspended").Error)
	require.ErrorIs(t, f.s.requireImportWriter(f.db.WithContext(f.ctx), "A", "kb", 1), access.ErrForbidden)
}
func TestFeishuV2PostgresInlinePublicationPinsConnectionThroughPrepare(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").
		Update("epoch", "reconnected").Error)
	_, err = f.s.ImportWithIdentity(
		f.ctx,
		"kb",
		"https://test.feishu.cn/file/real",
		types.FeishuUserOAuth,
		ref,
	)
	require.ErrorIs(t, err, ErrFeishuReconnect)
	require.Zero(t, f.downloads.Load())
	var attempts int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&attempts).Error)
	require.Zero(t, attempts)
}
