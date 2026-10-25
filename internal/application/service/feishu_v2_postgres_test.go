package service

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func v2PostgresFixture(t *testing.T) *personalRecoveryFixture {
	t.Helper()
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	f := newMVPFixture(t)
	ddl, err := os.ReadFile("../../../migrations/versioned/003039_feishu_access_v2.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	ddl, err = os.ReadFile("../../../migrations/versioned/003040_feishu_permission_collaboration.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	f.db = types.EnablePrivateKBGuards(f.db)
	f.s.db = f.db
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	cfg.Capabilities = &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true}
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(admin, cfg))
	return f
}

func TestFeishuV2PostgresSharedAdmissionRequiresRevisionApproval(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	links := []string{"https://test.feishu.cn/file/real"}
	preview, err := f.s.Preview(f.ctx, "kb", types.FeishuUserOAuth, ref.ConnectionID, links)
	require.NoError(t, err)
	require.Equal(t, "sharing_approval_required", preview[0].ErrorCategory)
	_, err = f.s.ImportWithIdentity(f.ctx, "kb", links[0], types.FeishuUserOAuth)
	require.ErrorIs(t, err, ErrFeishuSharingApproval)
	_, err = f.s.ApproveSharing(
		f.ctx,
		"kb",
		ref.ConnectionID,
		links[0],
		"controlled shared publication",
		false,
	)
	require.Error(t, err)
	approval, err := f.s.ApproveSharing(
		f.ctx,
		"kb",
		ref.ConnectionID,
		links[0],
		"controlled shared publication",
		true,
	)
	require.NoError(t, err)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", links[0], types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NotEmpty(t, m.AttemptID)
	// Revocation closes the existing attempt's next authorization, preserving
	// the original records instead of resetting any lease or cursor.
	require.NoError(
		t,
		f.db.Model(&types.FeishuSharingApproval{}).
			Where("id = ?", approval.ID).
			Update("state", "revoked").
			Error,
	)
	_, _, attempt, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	_, err = f.p.authorize(f.ctx, m, attempt)
	require.ErrorIs(t, err, ErrFeishuSharingApproval)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestFeishuV2PostgresCollaborationNeverGrantsOrRetries(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	view, err := f.s.CreateBatch(
		f.ctx,
		"kb",
		types.FeishuUserOAuth,
		ref.ConnectionID,
		"collaboration",
		[]string{"https://test.feishu.cn/file/real"},
	)
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", view.Items[0].Status)
	item := view.Items[0]
	row, err := f.s.Collaborate(
		f.ctx,
		view.Batch.ID,
		item.ID,
		"reported",
		"Document owner",
		"Owner reports access updated",
	)
	require.NoError(t, err)
	require.Equal(t, "reported_unverified", row.Result)
	row, err = f.s.Collaborate(f.ctx, view.Batch.ID, item.ID, "recheck", "Document owner", "")
	require.NoError(t, err)
	require.Equal(t, "sharing_approval_required", row.Result)
	_, err = f.s.Collaboration(f.userContext("B"), view.Batch.ID)
	require.Error(t, err)
	_, err = f.s.Collaborate(f.userContext("B"), view.Batch.ID, item.ID, "reported", "", "must deny")
	require.Error(t, err)
	_, err = f.s.Collaborate(f.ctx, view.Batch.ID, "another-item", "reported", "", "must deny")
	require.Error(t, err)
	rows, err := f.s.Collaboration(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var current types.FeishuBatchItem
	require.NoError(t, f.db.Where("id = ?", item.ID).First(&current).Error)
	require.Equal(t, "blocked", current.Status)
	require.Zero(t, current.RetryCount)
	var approvals int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&approvals).Error)
	require.Zero(t, approvals)
}

func TestFeishuV2PostgresBatchConcurrentIdempotencyAndAliasRecovery(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	links := []string{"https://test.feishu.cn/file/real", "https://test.feishu.cn/wiki/node"}
	var ids [2]string
	var errs [2]error
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view, e := f.s.CreateBatch(
				f.ctx,
				"kb",
				types.FeishuUserOAuth,
				ref.ConnectionID,
				"same-request",
				links,
			)
			errs[i] = e
			if view != nil {
				ids[i] = view.Batch.ID
			}
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, ids[0], ids[1])
	_, err = f.s.CreateBatch(f.ctx, "kb", types.FeishuUserOAuth, ref.ConnectionID, "same-request", links[:1])
	require.Error(t, err)
	// Two workers share the real PostgreSQL claim and canonical import locks.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := f.s.ReconcileBatches(context.Background()); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	view, err := f.s.Batch(f.ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, 1, view.Counts["duplicate"])
	require.Equal(t, 1, view.Counts["dispatched"])
	var imports int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.EqualValues(t, 1, imports)
	_, err = f.s.Batch(f.userContext("B"), ids[0])
	require.Error(t, err)
	// Replay after worker/reconciler restart cannot allocate another import.
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.EqualValues(t, 1, imports)
}
