package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuV2PostgresInlinePublicationPersistsBeforeRemoteWork(t *testing.T) {
	f := v2PostgresFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	original := f.s.newClient
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	links := []string{"https://test.feishu.cn/file/real"}
	view, err := f.s.QuickImport(f.ctx, "kb", "inline", links, true)
	require.NoError(t, err)
	require.NotNil(t, view.Batch.PublicationConsentAt)
	require.Zero(t, calls.Load(), "202 admission cannot wait for source metadata")
	replayed, err := f.s.QuickImport(f.ctx, "kb", "inline", links, true)
	require.NoError(t, err)
	require.Equal(t, view.Batch.ID, replayed.Batch.ID)
	_, err = f.s.QuickImport(f.ctx, "kb", "inline", links, false)
	require.Error(t, err, "idempotency must bind the explicit publication intent")
	// A temporary metadata failure leaves the durable intent and bounded retry intact.
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", current.Items[0].Status)
	require.Equal(t, 1, current.Items[0].RetryCount)
	require.Nil(t, current.Items[0].SharingApprovalID)
	f.s.newClient = original
	require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", current.Items[0].ID).
		Update("updated_at", time.Now().Add(-time.Minute)).Error)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", current.Items[0].Status)
	require.NotNil(t, current.Items[0].SharingApprovalID)
	var grant types.FeishuSharingApproval
	require.NoError(t, f.db.First(&grant, "id = ?", *current.Items[0].SharingApprovalID).Error)
	require.Equal(t, "1", grant.Revision)
	require.Equal(t, "A", grant.ApprovedBy)
	require.WithinDuration(t, view.Batch.PublicationConsentAt.Add(24*time.Hour), grant.ExpiresAt, time.Second)
}

func TestFeishuV2PostgresInlinePublicationNeverRebindsVersionOrRevocation(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(
		f.ctx,
		"kb",
		"version-bound",
		[]string{"https://test.feishu.cn/file/real"},
		true,
	)
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	item := current.Items[0]
	require.NotNil(t, item.SharingApprovalID)
	f.revision.Store(2)
	// Restart/retry does not grant the version observed later.
	require.NoError(t, f.s.bindBatchPublication(f.ctx, &current.Batch, &item))
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	var imported types.FeishuPersonalImport
	require.NoError(t, f.db.First(&imported, "id = ?", item.ImportID).Error)
	require.ErrorIs(
		t,
		f.s.checkSharingApproval(f.db.WithContext(f.ctx), cfg, &imported, "2"),
		ErrFeishuSharingApproval,
	)
	require.NoError(t, f.s.RevokeSharingApproval(f.ctx, "kb", *item.SharingApprovalID))
	require.NoError(t, f.s.bindBatchPublication(f.ctx, &current.Batch, &item))
	require.ErrorIs(
		t,
		f.s.checkSharingApproval(f.db.WithContext(f.ctx), cfg, &imported, "1"),
		ErrFeishuSharingApproval,
	)
	var grants []types.FeishuSharingApproval
	require.NoError(t, f.db.Find(&grants).Error)
	require.Len(t, grants, 1)
	require.Equal(t, "1", grants[0].Revision)
	require.Equal(t, "revoked", grants[0].State)
}

func TestFeishuV2PostgresInlinePublicationFencesLeaseAndLiveAuthority(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(f.ctx, "kb", "fenced", []string{"https://test.feishu.cn/file/real"}, true)
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
	require.NoError(
		t,
		f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").UpdateColumn("role", "viewer").Error,
	)
	require.ErrorIs(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item), access.ErrForbidden)
	var count int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(
		t,
		f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").UpdateColumn("role", "admin").Error,
	)
	old := time.Now().Add(-25 * time.Hour)
	view.Batch.PublicationConsentAt = &old
	require.ErrorIs(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item), errBatchPublicationExpired)
}

func TestFeishuV2PostgresInlineContinuePreservesOriginalBatch(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(f.ctx, "kb", "old-unconfirmed", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	require.Nil(t, view.Batch.PublicationConsentAt)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	blocked, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.True(t, blocked.CanContinuePublication)
	require.ErrorIs(t, f.s.ContinueBatch(f.ctx, view.Batch.ID, false), ErrFeishuSharingApproval)
	require.Error(t, f.s.ContinueBatch(f.userContext("B"), view.Batch.ID, true))
	require.NoError(t, f.s.ContinueBatch(f.ctx, view.Batch.ID, true))
	queued, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", queued.Items[0].Status)
	require.Equal(t, blocked.Items[0].ID, queued.Items[0].ID)
	require.Zero(t, queued.Items[0].RetryCount)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", current.Items[0].Status)
	require.False(t, current.CanContinuePublication)
	require.NoError(
		t,
		f.s.ContinueBatch(f.ctx, view.Batch.ID, true),
		"replayed action cannot reset downstream work",
	)
	for _, model := range []any{&types.FeishuBatch{}, &types.FeishuSharingApproval{}, &types.FeishuPersonalImport{}} {
		var count int64
		require.NoError(t, f.db.Model(model).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
}

func TestFeishuV2PostgresInlinePublicationRejectsWriteOnlyAndKeepsPrivate(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(
		t,
		f.db.Model(&types.TenantMember{}).Where("user_id = ?", "B").UpdateColumn("role", "contributor").Error,
	)
	links := []string{"https://test.feishu.cn/file/real"}
	_, err := f.s.QuickImport(f.userContext("B"), "kb", "writer-cannot-publish", links, true)
	require.Error(t, err, "stale admin claims cannot grant publication with only live write access")
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	view, err := f.s.QuickImport(f.ctx, "kb", "private-no-sharing", links, false)
	require.NoError(t, err)
	require.Nil(t, view.Batch.PublicationConsentAt)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	current, err := f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", current.Items[0].Status)
	var grants int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&grants).Error)
	require.Zero(t, grants)
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

func TestFeishuV2PostgresInlinePublicationCannotBorrowAnotherVersionApproval(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(
		f.ctx,
		"kb",
		"fixed-version",
		[]string{"https://test.feishu.cn/file/real"},
		true,
	)
	require.NoError(t, err)
	item := view.Items[0]
	item.Status, item.LeaseOwner = "processing", "worker-before-crash"
	until := time.Now().Add(time.Minute)
	item.LeaseUntil = &until
	require.NoError(t, f.db.Save(&item).Error)
	require.NoError(t, f.s.bindBatchPublication(f.ctx, &view.Batch, &item))
	require.NotNil(t, item.SharingApprovalID)
	f.revision.Store(2)
	_, err = f.s.ApproveSharing(
		f.ctx,
		"kb",
		view.Batch.ConnectionID,
		item.URL,
		"separate newer version approval",
		true,
	)
	require.NoError(t, err)
	// The original item must still fail before creating an attempt, even when
	// another explicit operation has approved the newer version to the same KB.
	_, err = f.s.ImportWithIdentity(
		batchPublicationContext(f.ctx, &item),
		"kb",
		item.URL,
		types.FeishuUserOAuth,
	)
	require.ErrorIs(t, err, ErrFeishuSharingApproval)
	var count int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "a retry cannot issue a third approval")
}
