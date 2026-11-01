package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Reproduce a legitimately sealed OLD execution, not a fabricated web file.
// No test changes live production data or skips source/authority validation.
func sealedExactRetryFixture(t *testing.T) (*personalRecoveryFixture, *types.FeishuPersonalAttempt, int64) {
	t.Helper()
	f, proof, ctx := exactExecutionFixture(t)
	require.NoError(t, f.p.beginExactPersonalReceipt(ctx, proof))
	owner := mustPersonalAttempt(t, f, proof.attempt)
	var op types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?",
		types.KnowledgeDispatchScope, proof.knowledge).Take(&op).Error)
	require.NoError(t, f.db.Model(&op).Update("op", "processing").Error)
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	_, resource, sealed, err := f.p.repo.Load(context.Background(), owner.ID)
	require.NoError(t, err)
	require.Equal(t, owner.Fence+1, resource.Generation)
	require.Equal(t, "needs_attention", sealed.Status)
	return f, sealed, resource.Generation
}

func TestPersonalExactRetrySourcePreflightDoesNotReopenSealedExecution(t *testing.T) {
	for _, change := range []string{"valid", "acl", "revision", "grant", "binding"} {
		t.Run(change, func(t *testing.T) {
			f, owner, sealed := sealedExactRetryFixture(t)
			switch change {
			case "acl":
				exactPreflightRemote(t, f, "1", false)
			case "revision":
				exactPreflightRemote(t, f, "2", true)
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			}
			before := exactPendingSnapshot(t, f, owner.ID)
			check, err := f.p.reviewExactRetrySource(context.Background(), owner, sealed)
			if change == "valid" {
				require.NoError(t, err)
				require.NotNil(t, check.validate)
			} else {
				require.Error(t, err)
				require.Nil(t, check)
			}
			require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
			require.ErrorIs(t, types.CheckLegacySourceRead(f.db, owner.LogicalKnowledgeID, "kb"),
				types.ErrLegacySourceRestricted)
		})
	}
}

func TestPersonalExactRetryClaimCASAndStageRollback(t *testing.T) {
	for _, change := range []string{"valid", "grant", "binding", "generation", "expired", "activation_failure"} {
		t.Run(change, func(t *testing.T) {
			f, owner, sealed := sealedExactRetryFixture(t)
			check, err := f.p.reviewExactRetrySource(context.Background(), owner, sealed)
			require.NoError(t, err)
			intent := types.PersonalExactRetry{
				Version: 1, SealedGeneration: sealed, Source: check.identity,
				NotAfter: time.Now().Add(time.Minute), Task: types.DocumentProcessPayload{},
			}
			row := types.TaskPendingOp{
				TenantID: owner.CallerTenantID, Scope: types.PersonalExactRetryScope,
				ScopeID: exactRetryKey(owner.LogicalKnowledgeID, sealed), TaskType: types.TypeDocumentProcess,
				Op: "pending", DedupKey: "new-retry",
			}
			require.NoError(t, f.db.Create(&row).Error)
			intent.Task.ExactRetryTicketID = row.ID
			if change == "expired" {
				intent.NotAfter = time.Now().Add(-time.Minute)
			}
			row.Payload, err = json.Marshal(intent)
			require.NoError(t, err)
			require.NoError(t, f.db.Model(&row).Update("payload", row.Payload).Error)
			require.NoError(t, f.db.Where("id=?", row.ID).Take(&row).Error)
			switch change {
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			case "generation":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_resources SET generation=generation+1").Error)
			}
			before := exactPendingSnapshot(t, f, owner.ID)
			activated := false
			fresh, err := f.p.repo.ClaimReviewedExactRetry(context.Background(), owner, &row, f.p.lease,
				func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
					return f.p.validateMutationAuthority(tx, current, check.validate)
				},
				func(_ *gorm.DB, current *types.FeishuPersonalAttempt) error {
					activated = true
					require.Equal(t, sealed+1, current.Fence)
					require.NotEmpty(t, current.OwnerNonce)
					require.NotNil(t, current.LeaseUntil)
					if change == "activation_failure" {
						return repository.ErrPersonalFence
					}
					return nil
				})
			if change != "valid" {
				require.Error(t, err)
				require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
				require.Equal(t, change == "activation_failure", activated)
				return
			}
			require.NoError(t, err)
			require.True(t, activated)
			require.Equal(t, sealed+1, fresh.Fence)
			var ticket types.TaskPendingOp
			require.NoError(t, f.db.Where("id=?", row.ID).Take(&ticket).Error)
			require.Equal(t, "claimed", ticket.Op)
			_, err = f.p.repo.ClaimReviewedExactRetry(context.Background(), owner, &row, f.p.lease,
				func(*gorm.DB, *types.FeishuPersonalAttempt) error { return nil },
				func(*gorm.DB, *types.FeishuPersonalAttempt) error {
					t.Fatal("late old delivery activated")
					return nil
				})
			require.Error(t, err)
		})
	}
}
