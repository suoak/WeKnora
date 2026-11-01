package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersonalExactRetryQuarantineAndClaimAreAtomic(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			f, owner, sealed := sealedExactRetryFixture(t)
			require.NoError(t, f.db.Table("knowledges").Where("id=?", owner.LogicalKnowledgeID).
				Update("enable_status", "disabled").Error)
			require.NoError(t, f.db.Exec(`ALTER TABLE embeddings ADD COLUMN id text PRIMARY KEY,
ADD COLUMN knowledge_base_id text,ADD COLUMN is_enabled boolean,ADD COLUMN source_type int,
ADD COLUMN source_id text,ADD COLUMN chunk_id text,ADD COLUMN embedding text`).Error)
			chunk := types.Chunk{
				ID: "old-unverified", TenantID: owner.CallerTenantID, KnowledgeBaseID: "kb",
				KnowledgeID: owner.LogicalKnowledgeID, Content: "private old unverified stage",
				ChunkType: types.ChunkTypeText,
			}
			require.NoError(t, f.db.Create(&chunk).Error)
			require.NoError(t, f.db.Exec("UPDATE chunks SET is_enabled=false WHERE id=?", chunk.ID).Error)
			require.NoError(t, f.db.Exec("INSERT INTO embeddings VALUES (?,?,'kb',false,0,?,?,?)",
				owner.LogicalKnowledgeID, "old-vector", chunk.ID, chunk.ID, "[0.1,0.2]").Error)
			check, err := f.p.reviewExactRetrySource(context.Background(), owner, sealed)
			require.NoError(t, err)
			stage, err := inspectExactRetryStage(f.db, check.identity, false)
			require.NoError(t, err)
			receipt, err := loadExactRetryStagingReceipt(f.db, check.identity, false)
			require.NoError(t, err)
			bytes, err := json.Marshal(receipt)
			require.NoError(t, err)
			intent := types.PersonalExactRetry{
				Version: 1, SealedGeneration: sealed, Source: check.identity,
				StageSHA256: stage.SHA256, SourceReceiptID: receipt.ID, SourceReceiptSHA: exactRetrySHA(bytes),
				NotAfter: time.Now().Add(time.Minute),
			}
			row := types.TaskPendingOp{
				TenantID: owner.CallerTenantID, Scope: types.PersonalExactRetryScope,
				ScopeID: exactRetryKey(owner.LogicalKnowledgeID, sealed), TaskType: types.TypeDocumentProcess,
				Op: "pending", DedupKey: "new-stage-retry",
			}
			require.NoError(t, f.db.Create(&row).Error)
			intent.Task.ExactRetryTicketID = row.ID
			row.Payload, err = json.Marshal(intent)
			require.NoError(t, err)
			require.NoError(t, f.db.Model(&row).Update("payload", row.Payload).Error)
			require.NoError(t, f.db.Where("id=?", row.ID).Take(&row).Error)
			before := exactPendingSnapshot(t, f, owner.ID)
			_, err = f.p.repo.ClaimReviewedExactRetry(context.Background(), owner, &row, f.p.lease,
				func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
					return f.p.validateMutationAuthority(tx, current, check.validate)
				},
				func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
					fresh := *check.checkedSource
					fresh.generation = current.Fence
					fresh.nonce = current.OwnerNonce
					fresh.leaseUntil = *current.LeaseUntil
					if err := f.p.quarantineExactRetryStage(tx, &fresh, intent, row.ID, owner); err != nil {
						return err
					}
					if rollback {
						return repository.ErrPersonalFence
					}
					return nil
				})
			var chunks, vectors, archives int64
			require.NoError(t, f.db.Table("chunks").Where("knowledge_id=?", owner.LogicalKnowledgeID).
				Count(&chunks).Error)
			require.NoError(t, f.db.Table("embeddings").Where("knowledge_id=?", owner.LogicalKnowledgeID).
				Count(&vectors).Error)
			require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope=?", types.PersonalExactArchiveScope).
				Count(&archives).Error)
			if rollback {
				require.ErrorIs(t, err, repository.ErrPersonalFence)
				require.EqualValues(t, 1, chunks)
				require.EqualValues(t, 1, vectors)
				require.Zero(t, archives)
				require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
				return
			}
			require.NoError(t, err)
			require.Zero(t, chunks)
			require.Zero(t, vectors)
			require.EqualValues(t, 1, archives)
			var archive types.TaskPendingOp
			require.NoError(t, f.db.Where("scope=?", types.PersonalExactArchiveScope).Take(&archive).Error)
			require.NotContains(t, string(archive.Payload), "private old unverified stage")
			var retained struct {
				Ciphertext string `json:"ciphertext"`
				SHA        string `json:"plaintext_sha256"`
			}
			require.NoError(t, json.Unmarshal(archive.Payload, &retained))
			plain, err := utils.DecryptStoredSecret(retained.Ciphertext)
			require.NoError(t, err)
			require.Equal(t, retained.SHA, exactRetrySHA([]byte(plain)))
			require.Contains(t, plain, "private old unverified stage")
			require.ErrorIs(t, types.CheckLegacySourceRead(f.db, owner.LogicalKnowledgeID, "kb"),
				types.ErrLegacySourceRestricted)
		})
	}
}
