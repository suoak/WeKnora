package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalPreflightDeferralPreservesDispatchAndFencesClaim(t *testing.T) {
	f, attempt, _ := pendingExactFixture(t)
	ctx := context.Background()
	before := exactPendingSnapshot(t, f, attempt.ID)
	require.NoError(t, f.p.repo.RecordPreflightDeferral(ctx, attempt, "embedded_dependency_unsupported"))
	current := mustPersonalAttempt(t, f, attempt.ID)
	require.Equal(t, "index_pending", current.Status)
	require.Equal(t, attempt.Fence, current.Fence)
	require.Empty(t, current.OwnerNonce)
	require.Nil(t, current.LeaseUntil)
	require.Equal(t, 1, current.RetryCount)
	require.True(t, current.RetryAfter.After(time.Now()))
	require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, attempt.ID)))
	_, err := f.p.repo.Claim(ctx, attempt.ID, time.Minute)
	require.ErrorIs(t, err, repository.ErrPersonalBusy)
	err = f.p.repo.RecordPreflightDeferral(ctx, attempt, "preflight_transient")
	require.ErrorIs(t, err, repository.ErrPersonalFence)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", attempt.ID).
		Update("retry_after", time.Now().Add(-time.Minute)).Error)
	owner, err := f.p.repo.Claim(ctx, attempt.ID, time.Minute)
	require.NoError(t, err)
	require.Greater(t, owner.Fence, attempt.Fence)
	err = f.p.repo.RecordPreflightDeferral(ctx, current, "preflight_transient")
	require.ErrorIs(t, err, repository.ErrPersonalFence)
}

func TestPersonalPreflightDeferralDoesNotBlockOtherDocuments(t *testing.T) {
	f, attempt, _ := pendingExactFixture(t)
	require.NoError(t, f.p.repo.RecordPreflightDeferral(context.Background(), attempt, "preflight_transient"))
	other, otherAttempt, intent := pendingExactFixture(t)
	ready, err := other.p.firstCreateDispatchable(context.Background(), intent)
	require.NoError(t, err)
	require.True(t, ready)
	_, err = other.p.repo.Claim(context.Background(), otherAttempt.ID, time.Minute)
	require.NoError(t, err)
}

func TestPersonalProcessingMetadataDistinguishesPreflightFromLiveOwner(t *testing.T) {
	f, attempt, _ := pendingExactFixture(t)
	h := NewHousekeepingService(f.db, nil, nil, nil)
	ctx := context.Background()
	require.NoError(t, f.p.repo.RecordPreflightDeferral(ctx, attempt, "embedded_dependency_unsupported"))
	states, err := h.ProcessingExecutions(ctx, []string{attempt.LogicalKnowledgeID})
	require.NoError(t, err)
	require.Equal(t, "blocked", states[attempt.LogicalKnowledgeID].State)
	require.Equal(t, "document_format_unsupported", states[attempt.LogicalKnowledgeID].Reason)
	require.Nil(t, states[attempt.LogicalKnowledgeID].LeaseUntil)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", attempt.ID).
		Update("retry_after", time.Now().Add(-time.Minute)).Error)
	_, err = f.p.repo.Claim(ctx, attempt.ID, time.Minute)
	require.NoError(t, err)
	states, err = h.ProcessingExecutions(ctx, []string{attempt.LogicalKnowledgeID})
	require.NoError(t, err)
	require.Equal(t, "running", states[attempt.LogicalKnowledgeID].State)
	require.NotNil(t, states[attempt.LogicalKnowledgeID].LeaseUntil)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", attempt.ID).
		Update("lease_until", time.Now().Add(-time.Minute)).Error)
	states, err = h.ProcessingExecutions(ctx, []string{attempt.LogicalKnowledgeID})
	require.NoError(t, err)
	require.NotEqual(t, "running", states[attempt.LogicalKnowledgeID].State)
}
