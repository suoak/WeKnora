package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Dispatch metadata is not permission to read a document or ignore sibling holds.
// Real PostgreSQL, durable first-create protocol and private manifest are used;
// the Feishu provider is synthetic. No production records are imported or edited.
func TestPersonalRecoveryHeldSiblingMetadataDispatchDoesNotGrantContent(t *testing.T) {
	for _, state := range []string{"hold", "revoked"} {
		t.Run(state, func(t *testing.T) {
			f := newMVPFixture(t)
			m := f.prepare("https://tenant.feishu.cn/file/real")
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			k, op, _ := mvpPublicTask(t, f, m)
			var intent types.KnowledgeDispatch
			require.NoError(t, json.Unmarshal(op.Payload, &intent))
			currentImport, before, attempt, err := f.p.repo.Load(
				context.Background(),
				intent.AttemptID,
			)
			require.NoError(t, err)
			require.EqualValues(t, 3, before.Generation)
			require.Equal(t, "index_pending", attempt.Status)
			manifest, err := f.p.store.read(attempt)
			require.NoError(t, err)
			require.Equal(t, attempt.ManifestHash, manifest.Hash)
			// Test fixture setup mirrors the observed public-document outbox.
			require.NoError(
				t,
				f.db.Exec(
					`UPDATE task_pending_ops SET op='pending',claimed_at=NULL,fail_count=0 WHERE id=?`,
					op.ID,
				).Error,
			)
			require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources
 (id text PRIMARY KEY, tenant_id bigint,knowledge_base_id text,knowledge_id text,state text)`).Error)
			require.NoError(t, f.db.Create(&types.DataSource{
				ID: "old-drive", TenantID: 1, KnowledgeBaseID: "kb", Name: "old-drive",
				Type: "feishu_drive", Status: "error",
			}).Error)
			require.NoError(
				t,
				f.db.Exec(
					`UPDATE data_sources SET legacy_access_state=? WHERE id='old-drive'`,
					state,
				).Error,
			)
			types.EnableLegacyReadGuards(f.db)

			// A metadata projection can inspect the durable intent without reading
			// a protected document or granting access to any content.
			ctx, err := f.p.privateWorkerContext(context.Background(), m, attempt)
			require.NoError(t, err)
			require.Zero(t, types.WorkspaceWebReadTenant(ctx))
			projection, projectionErr := f.p.loadDispatchMetadata(
				ctx,
				k.TenantID,
				k.KnowledgeBaseID,
				k.ID,
			)
			require.NoError(t, projectionErr)
			require.Equal(t, attempt.ID, projection.AttemptID)
			require.Equal(t, attempt.ManifestHash, projection.ManifestSHA256)
			require.Equal(t, before.Generation, attempt.Fence)
			ready, err := f.p.firstCreateDispatchable(ctx, intent)
			require.NoError(t, err)
			require.True(t, ready)
			var protected types.Knowledge
			require.ErrorIs(t, f.db.WithContext(ctx).Where("id=?", k.ID).First(&protected).Error,
				types.ErrLegacySourceRestricted)
			// Reconciliation of an unprocessed intent reads only metadata, too.
			require.NoError(t, f.p.reconcileFirstCreatePublic(ctx, currentImport, attempt))

			// No Claim or terminal failure occurred. claimed_at is a reserved
			// outbox column, so the unchanged generation/status is also checked.
			_, after, current, err := f.p.repo.Load(ctx, attempt.ID)
			require.NoError(t, err)
			require.Equal(t, before.Generation, after.Generation)
			require.Equal(t, attempt.Status, current.Status)
			require.Nil(t, current.LeaseUntil)
			require.Empty(t, current.OwnerNonce)
			var stored types.TaskPendingOp
			require.NoError(t, f.db.Where("id=?", op.ID).First(&stored).Error)
			require.Equal(t, "pending", stored.Op)
			require.Zero(t, stored.FailCount)
			require.Nil(t, stored.ClaimedAt)
			var count int64
			require.NoError(
				t,
				f.db.Model(&types.Chunk{}).Where("knowledge_id=?", k.ID).Count(&count).Error,
			)
			require.Zero(t, count)
			// A processed handoff without serving proof must not be newly
			// claimed or relabeled as an index failure by reconciliation.
			require.NoError(
				t,
				f.db.Exec("UPDATE task_pending_ops SET op='processed' WHERE id=?", op.ID).Error,
			)
			require.ErrorIs(
				t,
				f.p.reconcileFirstCreatePublic(ctx, currentImport, attempt),
				types.ErrLegacySourceRestricted,
			)
			_, after, current, err = f.p.repo.Load(ctx, attempt.ID)
			require.NoError(t, err)
			require.Equal(t, before.Generation, after.Generation)
			require.Equal(t, "index_pending", current.Status)
			require.Empty(t, current.OwnerNonce)
			require.Nil(t, current.LeaseUntil)
		})
	}
}

func TestPersonalRecoveryHeldSiblingModelGateIsIndependent(t *testing.T) {
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	_, _, a, err := f.p.repo.Load(context.Background(), m.AttemptID)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources
 (id text PRIMARY KEY, tenant_id bigint,knowledge_base_id text,knowledge_id text,state text)`).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO feishu_legacy_resources VALUES
 ('old-resource',1,'old-kb','no-published-knowledge','hold')`).Error)
	types.EnableLegacyReadGuards(f.db)
	// Current user/KB/OAuth identity can be valid while an unrelated tenant
	// resource still blocks model input. Never infer model permission from a
	// successful destination grant or a queue acknowledgment.
	work, err := f.p.authorize(context.Background(), m, a)
	require.NoError(t, err)
	gate := repository.NewModelRepository(f.db).(interface {
		CheckLegacyModelInputs(context.Context, uint64) error
	})
	require.ErrorIs(t, gate.CheckLegacyModelInputs(work, 1), types.ErrLegacySourceRestricted)
}
