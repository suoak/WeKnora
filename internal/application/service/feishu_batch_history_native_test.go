//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func assertExactPublicationHistory(t *testing.T, f *personalRecoveryFixture, imported string) {
	t.Helper()
	batch := types.FeishuBatch{OwnerID: "A", KBID: "kb", TenantID: 1}
	states, err := f.s.exactBatchHistoryStates(context.Background(), batch, []string{imported})
	require.NoError(t, err)
	require.Equal(t, "succeeded", states[imported], "current exact publication supersedes old admission failure")
	var source types.FeishuPersonalImport
	require.NoError(t, f.db.Where("id=?", imported).Take(&source).Error)
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		source.KnowledgeID).Take(&receipt).Error)
	var proof types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &proof))
	proof.Generation++
	bad, err := json.Marshal(proof)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("id=?", receipt.ID).
		UpdateColumn("payload", types.JSON(bad)).Error)
	_, err = f.s.exactBatchHistoryStates(context.Background(), batch, []string{imported})
	require.Error(t, err, "stale generation cannot manufacture a successful history entry")
	require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("id=?", receipt.ID).
		UpdateColumn("payload", types.JSON(receipt.Payload)).Error)
	strategy, err := json.Marshal(types.IndexingStrategy{WikiEnabled: true})
	require.NoError(t, err)
	var original struct{ IndexingStrategy types.IndexingStrategy }
	require.NoError(t, f.db.Table("knowledge_bases").Where("id='kb'").Take(&original).Error)
	defer func() {
		encoded, err := json.Marshal(original.IndexingStrategy)
		require.NoError(t, err)
		require.NoError(t, f.db.Table("knowledge_bases").Where("id='kb'").
			Update("indexing_strategy", string(encoded)).Error)
	}()
	require.NoError(t, f.db.Table("knowledge_bases").Where("id='kb'").
		Update("indexing_strategy", string(strategy)).Error)
	states, err = f.s.exactBatchHistoryStates(context.Background(), batch, []string{imported})
	require.NoError(t, err)
	require.Equal(t, "dispatched", states[imported], "an index receipt does not certify Wiki publication")
}
