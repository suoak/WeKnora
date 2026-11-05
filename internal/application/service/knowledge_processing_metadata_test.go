package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestProcessingMetadataIsIndependentOfStagingReceiptAndNeverGrantsBody(t *testing.T) {
	f, attempt, _ := pendingExactFixture(t)
	ctx := types.WithCaller(f.ctx, types.Caller{TenantID: 1, UserID: "A", Role: types.TenantRoleAdmin})
	_, err := f.ks.GetKnowledgeProcessingMetadata(ctx, attempt.LogicalKnowledgeID)
	require.Error(t, err, "ordinary context cannot ask for this projection")
	ctx = types.WithKnowledgeProcessingMetadata(ctx, attempt.LogicalKnowledgeID)
	row, err := f.ks.GetKnowledgeByIDOnly(ctx, attempt.LogicalKnowledgeID)
	require.NoError(t, err)
	require.True(t, row.MetadataOnly)
	require.Equal(t, attempt.LogicalKnowledgeID, row.ID)
	require.Empty(t, row.Description)
	require.Empty(t, row.FilePath)
	require.Empty(t, row.Metadata)
	require.Nil(t, row.Profile)
	require.NoError(t, f.db.Exec("UPDATE knowledges SET parse_status=?,description=? WHERE id=?",
		"processing", "secret-unpublished", row.ID).Error)
	// A staging receipt must still not be mistaken for published content.
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		TenantID: 1, TaskType: "private", Scope: types.PersonalExactReceiptScope,
		ScopeID: row.ID, DedupKey: "receipt", Op: "staging", Payload: types.JSON(`{"secret":"unpublished"}`),
	}).Error)
	row, err = f.ks.GetKnowledgeByIDOnly(ctx, attempt.LogicalKnowledgeID)
	require.NoError(t, err)
	require.Equal(t, "processing", row.ParseStatus)
	data, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret")
	_, err = f.ks.GetKnowledgeByIDOnly(f.ctx, attempt.LogicalKnowledgeID)
	require.Error(t, err, "a staging receipt still grants no ordinary read")
	_, err = f.ks.GetKnowledgeProcessingMetadata(context.Background(), attempt.LogicalKnowledgeID)
	require.Error(t, err)
}
