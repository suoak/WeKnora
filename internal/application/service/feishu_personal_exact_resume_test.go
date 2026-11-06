package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func closedExactResumeFixture(t *testing.T) (*personalRecoveryFixture, *types.FeishuPersonalAttempt) {
	t.Helper()
	f, a, intent := pendingExactFixture(t)
	renameExactProtocolFixture(t, f, a, &intent, "resume.docx", "docx")
	require.NoError(t, f.db.Exec("ALTER TABLE embeddings ADD COLUMN is_enabled boolean DEFAULT false").Error)
	require.NoError(t, f.db.AutoMigrate(&types.TaskDeadLetter{}))
	owner, err := f.p.repo.Claim(context.Background(), a.ID, f.p.lease)
	require.NoError(t, err)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	ctx, err := f.p.exactDocumentExecutionContext(context.Background(), proof)
	require.NoError(t, err)
	require.NoError(t, f.p.beginExactPersonalReceipt(ctx, proof))
	require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET op='processing' WHERE scope=? AND scope_id=?",
		types.KnowledgeDispatchScope, a.LogicalKnowledgeID).Error)
	var op types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.KnowledgeDispatchScope,
		a.LogicalKnowledgeID).Take(&op).Error)
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	return f, mustPersonalAttempt(t, f, a.ID)
}

func TestPersonalExactResumeRetainsOriginalIdentityAndConsumesNewClaim(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	check := &personalExactPreflight{observed: observed, closed: true}
	proof, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
	require.NoError(t, err)
	require.Nil(t, proof, "closed preflight must not issue execution authority")
	require.NotNil(t, check.source)
	var before []types.TaskPendingOp
	require.NoError(t, f.db.Order("id").Find(&before).Error)
	require.NoError(t, f.p.repo.QueueExactResume(context.Background(), observed,
		exactSourceReceipt(check.source), check.validate))
	require.ErrorIs(t, f.p.repo.QueueExactResume(context.Background(), observed,
		exactSourceReceipt(check.source), check.validate), repository.ErrPersonalFence)
	var after []types.TaskPendingOp
	require.NoError(t, f.db.Order("id").Find(&after).Error)
	require.Len(t, after, len(before))
	for i := range before {
		require.Equal(t, before[i].ID, after[i].ID)
		require.Equal(t, before[i].DedupKey, after[i].DedupKey)
	}
	current := mustPersonalAttempt(t, f, observed.ID)
	require.Equal(t, "index_pending", current.Status)
	require.Equal(t, observed.LogicalKnowledgeID, current.LogicalKnowledgeID)
	require.Equal(t, observed.Fence+1, current.Fence)
	require.Equal(t, observed.RetryCount+1, current.RetryCount)
	preflight, err := f.p.preflightExactPersonalDocument(context.Background(), current)
	require.NoError(t, err)
	owner, err := f.p.claimExactPreflight(context.Background(), preflight)
	require.NoError(t, err)
	require.Equal(t, observed.Fence+2, owner.Fence)
	require.NotEmpty(t, owner.OwnerNonce)
	fresh, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, f.p.repo.ActivateExactResume(context.Background(), owner,
		exactSourceReceipt(fresh)))
	require.Error(t, f.p.repo.ActivateExactResume(context.Background(), owner,
		exactSourceReceipt(fresh)), "activation cannot be replayed")
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		owner.LogicalKnowledgeID).Take(&receipt).Error)
	require.Equal(t, "staging", receipt.Op)
	var source types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &source))
	require.Equal(t, owner.Fence, source.Generation)
	var count int64
	require.NoError(t, f.db.Model(&types.TaskDeadLetter{}).
		Where("task_type=?", "knowledge:exact_resume_audit").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestPersonalExactResumeRefusesRevokedOrChangedSource(t *testing.T) {
	for _, change := range []string{"grant", "remote_permission", "revision", "limit"} {
		t.Run(change, func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			switch change {
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "remote_permission":
				exactPreflightRemote(t, f, "1", false)
			case "revision":
				exactPreflightRemote(t, f, "2", true)
			case "limit":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_count=3").Error)
				observed = mustPersonalAttempt(t, f, observed.ID)
			}
			before := exactPendingSnapshot(t, f, observed.ID)
			check := &personalExactPreflight{observed: observed, closed: true}
			_, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
			if err == nil {
				err = f.p.repo.QueueExactResume(context.Background(), observed,
					exactSourceReceipt(check.source), check.validate)
			}
			require.Error(t, err)
			require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, observed.ID)))
		})
	}
}

func TestPersonalExactResumeConcurrentPreparationHasOneWinner(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	check := &personalExactPreflight{observed: observed, closed: true}
	_, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
	require.NoError(t, err)
	source := exactSourceReceipt(check.source)
	var pending sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		pending.Add(1)
		go func() {
			defer pending.Done()
			results <- f.p.repo.QueueExactResume(context.Background(), observed, source, check.validate)
		}()
	}
	pending.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result == nil {
			winners++
		} else {
			require.ErrorIs(t, result, repository.ErrPersonalFence)
		}
	}
	require.Equal(t, 1, winners)
}
