//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Build a genuine failed execution from the original v3/file fixture. Only
// this disposable test schema is written. The source provider is synthetic;
// configured real models, normal queues/consumer and PostgreSQL are unchanged.
func prepareOriginalSealedRetryAcceptance(
	t *testing.T, f *personalRecoveryFixture, payload types.DocumentProcessPayload,
) types.DocumentProcessPayload {
	t.Helper()
	f.p.lease = 2 * time.Minute
	var op types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?",
		types.KnowledgeDispatchScope, payload.KnowledgeID).Take(&op).Error)
	_, _, pending, err := f.p.repo.Load(context.Background(), payload.PersonalAttemptID)
	require.NoError(t, err)
	preflight, err := f.p.preflightExactPersonalDocument(context.Background(), pending)
	require.NoError(t, err)
	owner, err := f.p.claimExactPreflight(context.Background(), preflight)
	require.NoError(t, err)
	require.EqualValues(t, 4, owner.Fence)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	ctx, err := f.p.exactDocumentExecutionContext(context.Background(), proof)
	require.NoError(t, err)
	require.NoError(t, f.p.beginExactPersonalReceipt(ctx, proof))
	require.NoError(t, types.WithExactDocumentWrite(ctx, f.db, proof.tenant, proof.knowledge, proof.kb, func(
		tx *gorm.DB,
	) error {
		if err := tx.Table("knowledges").Where("id=?", proof.knowledge).
			Update("enable_status", "disabled").Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,is_enabled,chunk_type)
VALUES('old-private-stage',?,?,?,'old unverified output must never enter a model',false,'text')`,
			proof.tenant, proof.kb, proof.knowledge).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO embeddings(
source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,embedding,is_enabled)
VALUES('old-private-stage',0,'old-private-stage',?,?,'old unverified output must never enter a model',
1024,('[1,'||repeat('0,',1022)||'0]')::halfvec,false)`, proof.knowledge, proof.kb).Error
	}))
	require.NoError(t, f.db.Model(&op).Update("op", "processing").Error)
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	_, r, sealed, err := f.p.repo.Load(context.Background(), owner.ID)
	require.NoError(t, err)
	require.EqualValues(t, 5, r.Generation)
	require.Empty(t, sealed.OwnerNonce)
	require.Nil(t, sealed.LeaseUntil)
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY", "1")
	policy, err := json.Marshal(map[string]int64{payload.KnowledgeID: r.Generation})
	require.NoError(t, err)
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS", string(policy))
	t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL", time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339))
	ticket, err := f.p.requestExactRetry(context.Background(), payload.KnowledgeID, true)
	require.NoError(t, err)
	_, intent, err := f.p.loadExactRetry(context.Background(), ticket.ID)
	require.NoError(t, err)
	require.EqualValues(t, 5, intent.SealedGeneration)
	_, after, still, err := f.p.repo.Load(context.Background(), owner.ID)
	require.NoError(t, err)
	require.EqualValues(t, 5, after.Generation)
	require.Empty(t, still.OwnerNonce)
	require.Nil(t, still.LeaseUntil)
	return intent.Task
}
