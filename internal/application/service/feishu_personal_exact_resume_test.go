package service

import (
	"context"
	"encoding/json"
	"fmt"
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
	housekeeping := &HousekeepingService{db: f.db}
	states, err := housekeeping.ProcessingExecutions(context.Background(),
		[]string{observed.LogicalKnowledgeID})
	require.NoError(t, err)
	require.Equal(t, "blocked", states[observed.LogicalKnowledgeID].State)
	require.Nil(t, states[observed.LogicalKnowledgeID].LeaseUntil)
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
	states, err = housekeeping.ProcessingExecutions(context.Background(),
		[]string{observed.LogicalKnowledgeID})
	require.NoError(t, err)
	require.Equal(t, "retrying", states[observed.LogicalKnowledgeID].State)
	preflight, err := f.p.preflightExactPersonalDocument(context.Background(), current)
	require.NoError(t, err)
	owner, err := f.p.claimExactPreflight(context.Background(), preflight)
	require.NoError(t, err)
	require.Equal(t, observed.Fence+2, owner.Fence)
	require.NotEmpty(t, owner.OwnerNonce)
	states, err = housekeeping.ProcessingExecutions(context.Background(),
		[]string{observed.LogicalKnowledgeID})
	require.NoError(t, err)
	require.Equal(t, "running", states[observed.LogicalKnowledgeID].State)
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

func TestPersonalExactResumeRefusesRevokedOrCorruptedSource(t *testing.T) {
	for _, change := range []string{"grant", "remote_permission", "hash", "limit"} {
		t.Run(change, func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			switch change {
			case "grant":
				err := f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error
				require.NoError(t, err)
			case "remote_permission":
				exactPreflightRemote(t, f, "1", false)
			case "hash":
				require.NoError(t, f.db.Exec("UPDATE resources SET content_hash=repeat('a',64)").Error)
			case "limit":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_count=?",
					repository.PersonalExactResumeMaxAttempts).Error)
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

func TestPersonalExactResumeMissingReceiptRequiresZeroOutput(t *testing.T) {
	for _, output := range []string{"none", "chunk", "vector"} {
		t.Run(output, func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			require.NoError(t, f.db.Exec("DELETE FROM task_pending_ops WHERE scope=? AND scope_id=?",
				types.PersonalExactReceiptScope, observed.LogicalKnowledgeID).Error)
			if output == "chunk" {
				require.NoError(t, f.db.Create(&types.Chunk{
					ID: "unknown", TenantID: 1,
					KnowledgeID: observed.LogicalKnowledgeID, KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeText,
				}).Error)
			}
			if output == "vector" {
				require.NoError(t, f.db.Exec("INSERT INTO embeddings(knowledge_id,is_enabled) VALUES(?,false)",
					observed.LogicalKnowledgeID).Error)
			}
			check := &personalExactPreflight{observed: observed, closed: true}
			_, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
			require.NoError(t, err)
			err = f.p.repo.QueueExactResume(context.Background(), observed,
				exactSourceReceipt(check.source), check.validate)
			var receipts int64
			require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?",
				types.PersonalExactReceiptScope, observed.LogicalKnowledgeID).Count(&receipts).Error)
			if output != "none" {
				require.Error(t, err)
				require.Zero(t, receipts)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, receipts)
			var receipt types.TaskPendingOp
			require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
				observed.LogicalKnowledgeID).Take(&receipt).Error)
			require.Equal(t, "resuming", receipt.Op)
			var audit types.TaskDeadLetter
			require.NoError(t, f.db.Where("task_type=?", "knowledge:exact_resume_audit").Take(&audit).Error)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(audit.Payload, &fields))
			require.JSONEq(t, "true", string(fields["receipt_missing_before_resume"]))
		})
	}
}

func TestPersonalExactReviewedResumeIsScopedAndCumulative(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset int
		listed bool
		want   bool
	}{
		{"unlisted", 0, false, false}, {"reviewed", 0, true, true}, {"exhausted", 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			count := repository.PersonalExactResumeMaxAttempts + tc.offset
			require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_count=? WHERE id=?",
				count, observed.ID).Error)
			observed = mustPersonalAttempt(t, f, observed.ID)
			ids := "unrelated"
			if tc.listed {
				ids = observed.LogicalKnowledgeID
			}
			t.Setenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_KNOWLEDGE_IDS", ids)
			check := &personalExactPreflight{observed: observed, closed: true}
			_, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
			require.NoError(t, err)
			err = f.p.repo.QueueExactResume(context.Background(), observed,
				exactSourceReceipt(check.source), check.validate)
			if !tc.want {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, count+1, mustPersonalAttempt(t, f, observed.ID).RetryCount)
			var audit types.TaskDeadLetter
			require.NoError(t, f.db.Where("task_type=?", "knowledge:exact_resume_audit").Take(&audit).Error)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(audit.Payload, &fields))
			require.JSONEq(t, "true", string(fields["reviewed_extra_attempt"]))
		})
	}
}

func TestPersonalExactReviewedHistoricalCounterResume(t *testing.T) {
	for _, baseline := range []int{161, 162, 163} {
		t.Run(fmt.Sprint(baseline), func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_count=162 WHERE id=?",
				observed.ID).Error)
			observed = mustPersonalAttempt(t, f, observed.ID)
			settings, err := json.Marshal(map[string]int{observed.LogicalKnowledgeID: baseline})
			require.NoError(t, err)
			t.Setenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_BASELINES", string(settings))
			check := &personalExactPreflight{observed: observed, closed: true}
			_, err = f.p.checkExactPersonalSource(context.Background(), observed, false, check)
			require.NoError(t, err)
			err = f.p.repo.QueueExactResume(context.Background(), observed,
				exactSourceReceipt(check.source), check.validate)
			if baseline != 162 {
				require.ErrorIs(t, err, repository.ErrPersonalFence)
				require.Equal(t, 162, mustPersonalAttempt(t, f, observed.ID).RetryCount)
				return
			}
			require.NoError(t, err)
			current := mustPersonalAttempt(t, f, observed.ID)
			require.Equal(t, 163, current.RetryCount)
			require.Less(t, repository.PersonalExactResumeBudget(current.LogicalKnowledgeID, current.RetryCount),
				current.RetryCount, "consumed review must not admit another execution")
		})
	}
}

func TestPersonalExactResumeUsesImportedSnapshotAfterRemoteEdit(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	exactPreflightRemote(t, f, "2", true)
	check := &personalExactPreflight{observed: observed, closed: true}
	_, err := f.p.checkExactPersonalSource(context.Background(), observed, false, check)
	require.NoError(t, err)
	require.Equal(t, observed.Revision, check.source.revision)
	require.NoError(t, f.p.repo.QueueExactResume(context.Background(), observed,
		exactSourceReceipt(check.source), check.validate))
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

func TestPersonalExactResumeWorkerLostBeforeActivationKeepsOriginalIntent(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	queue := func(current *types.FeishuPersonalAttempt) {
		check := &personalExactPreflight{observed: current, closed: true}
		_, err := f.p.checkExactPersonalSource(context.Background(), current, false, check)
		require.NoError(t, err)
		require.NoError(t, f.p.repo.QueueExactResume(context.Background(), current,
			exactSourceReceipt(check.source), check.validate))
	}
	claim := func() *types.FeishuPersonalAttempt {
		current := mustPersonalAttempt(t, f, observed.ID)
		preflight, err := f.p.preflightExactPersonalDocument(context.Background(), current)
		require.NoError(t, err)
		owner, err := f.p.claimExactPreflight(context.Background(), preflight)
		require.NoError(t, err)
		return owner
	}
	var original types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.KnowledgeDispatchScope,
		observed.LogicalKnowledgeID).Take(&original).Error)
	queue(observed)
	lost := claim()
	// A stopped worker has not activated its receipt or changed any derived row.
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), lost, original.ID))
	queue(mustPersonalAttempt(t, f, observed.ID))
	owner := claim()
	fresh, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, f.p.repo.ActivateExactResume(context.Background(), owner,
		exactSourceReceipt(fresh)))
	var current types.TaskPendingOp
	require.NoError(t, f.db.First(&current, original.ID).Error)
	require.Equal(t, original.ID, current.ID)
	require.Equal(t, original.DedupKey, current.DedupKey)
	require.JSONEq(t, string(original.Payload), string(current.Payload))
	require.Equal(t, observed.RetryCount+2, owner.RetryCount)
	require.Equal(t, observed.Fence+4, owner.Fence)
}
