package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestPersonalRecoveryExactExecutionAbortCannotCancelNewOwner(t *testing.T) {
	f, proof, _ := exactExecutionFixture(t)
	owner := mustPersonalAttempt(t, f, proof.attempt)
	var op types.TaskPendingOp
	require.NoError(
		t,
		f.db.Where("scope=? AND scope_id=?", types.KnowledgeDispatchScope, proof.knowledge).Take(&op).Error,
	)
	dispatch := f.ks.repo.(interfaces.KnowledgeDispatchRepository)
	require.NoError(t, dispatch.SetKnowledgeDispatchState(context.Background(), op.ID, "processing"))
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_attempts SET lease_until=now()-interval '1 second' WHERE id=?",
			owner.ID,
		).Error,
	)
	newOwner, err := f.p.repo.Claim(context.Background(), owner.ID, f.p.lease)
	require.NoError(t, err)
	require.Greater(t, newOwner.Fence, owner.Fence)
	require.ErrorIs(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID), repository.ErrPersonalFence)
	require.ErrorIs(t, f.p.closeExactReconcileExecution(context.Background(), owner), repository.ErrPersonalFence)
	current := mustPersonalAttempt(t, f, owner.ID)
	require.Equal(t, newOwner.Fence, current.Fence)
	require.Equal(t, newOwner.OwnerNonce, current.OwnerNonce)
	require.Equal(t, "processing", current.Status)
	currentOp, err := dispatch.GetKnowledgeDispatch(context.Background(), proof.tenant, proof.knowledge)
	require.NoError(t, err)
	require.Equal(t, "processing", currentOp.Op)
}

func TestPersonalRecoveryExactExecutionAbortExpiredRevokedOwnerIsMetadataOnly(t *testing.T) {
	f, proof, _ := exactExecutionFixture(t)
	owner := mustPersonalAttempt(t, f, proof.attempt)
	var op types.TaskPendingOp
	require.NoError(
		t,
		f.db.Where("scope=? AND scope_id=?", types.KnowledgeDispatchScope, proof.knowledge).Take(&op).Error,
	)
	dispatch := f.ks.repo.(interfaces.KnowledgeDispatchRepository)
	require.NoError(t, dispatch.SetKnowledgeDispatchState(context.Background(), op.ID, "processing"))
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_attempts SET lease_until=now()-interval '1 second' WHERE id=?",
			owner.ID,
		).Error,
	)
	require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	var attempt types.FeishuPersonalAttempt
	require.NoError(t, f.db.Where("id=?", owner.ID).Take(&attempt).Error)
	require.Equal(t, "needs_attention", attempt.Status)
	require.Empty(t, attempt.OwnerNonce)
	require.Nil(t, attempt.LeaseUntil)
	var knowledge struct{ ParseStatus, Description string }
	require.NoError(
		t,
		f.db.Table("knowledges").
			Select("parse_status,description").
			Where("id=?", proof.knowledge).
			Take(&knowledge).
			Error,
	)
	require.Equal(t, types.ParseStatusPending, knowledge.ParseStatus)
	require.Empty(t, knowledge.Description)
	currentOp, err := dispatch.GetKnowledgeDispatch(context.Background(), proof.tenant, proof.knowledge)
	require.NoError(t, err)
	require.Equal(t, "attention", currentOp.Op)
	imported, _, _, err := f.p.repo.Load(context.Background(), proof.attempt)
	require.NoError(t, err)
	imported.AttemptID = proof.attempt
	statuses := []types.FeishuPersonalImport{*imported}
	require.NoError(t, f.p.firstCreateStatuses(context.Background(), statuses))
	require.Equal(t, "attention", statuses[0].IndexStatus)
	require.Equal(t, "needs_attention", statuses[0].Status)
}

func TestPersonalRecoveryExactExecutionAbortRejectsUnclaimedOperation(t *testing.T) {
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	_, op, _ := mvpPublicTask(t, f, m)
	attempt := mustPersonalAttempt(t, f, m.AttemptID)
	require.Empty(t, attempt.OwnerNonce)
	attempt.OwnerNonce = "forged-unclaimed-owner"
	require.Error(t, f.p.repo.AbortExactExecution(context.Background(), attempt, op.ID))
	current := mustPersonalAttempt(t, f, m.AttemptID)
	require.NotEqual(t, "needs_attention", current.Status)
	require.Empty(t, current.OwnerNonce)
}
