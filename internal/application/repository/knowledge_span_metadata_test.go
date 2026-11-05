package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestProcessingSpanMetadataDoesNotLoadOrExposeUnpublishedContent(t *testing.T) {
	repo, db := setupSpanTestRepo(t)
	now := time.Now()
	rows := []types.KnowledgeProcessingSpan{
		{
			KnowledgeID: "doc", Attempt: 1, SpanID: "secret-root-id", Name: "secret-root-name",
			Kind: types.SpanKindRoot, Status: types.SpanStatusRunning, StartedAt: &now,
		},
		{
			KnowledgeID: "doc", Attempt: 1, SpanID: "secret-child-id", ParentSpanID: "secret-root-id",
			Name: "multimodal.image[2]", Kind: types.SpanKindSubSpan, Status: types.SpanStatusDone,
			Input: types.JSONMap{"body": "secret-input"}, Output: types.JSONMap{"body": "secret-output"},
			Metadata: types.JSONMap{"token": "secret-metadata"}, ErrorMessage: "secret-error",
			ErrorDetail: "secret-stack", DurationMs: 123,
		},
	}
	require.NoError(t, db.Create(&rows).Error)
	ctx := types.WithKnowledgeProcessingMetadata(context.Background(), "doc")
	metadata, err := repo.ListByAttempt(ctx, "doc", 1)
	require.NoError(t, err)
	require.Len(t, metadata, 2)
	require.Equal(t, metadata[0].SpanID, metadata[1].ParentSpanID)
	require.Equal(t, "multimodal.image[2]", metadata[1].Name)
	require.EqualValues(t, 123, metadata[1].DurationMs)
	data, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret")
	// Normal reads retain their existing full diagnostic contract.
	full, err := repo.ListByAttempt(context.Background(), "doc", 1)
	require.NoError(t, err)
	require.Equal(t, "secret-input", full[1].Input["body"])
	require.Equal(t, "secret-error", full[1].ErrorMessage)
	// A context scoped to another document is not a metadata capability for doc.
	other := types.WithKnowledgeProcessingMetadata(context.Background(), "other")
	full, err = repo.ListByAttempt(other, "doc", 1)
	require.NoError(t, err)
	require.Equal(t, "secret-input", full[1].Input["body"])
}
