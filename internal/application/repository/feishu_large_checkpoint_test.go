package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyLargeCheckpointResourceBoundsAndClaimBudget(t *testing.T) {
	c := types.FeishuLegacyEmbeddingCheckpoint{
		Version: 2, RequestID: "server-issued-request", SourceConfig: strings.Repeat("1", 64),
		Inputs: make([]types.FeishuLegacyEmbeddingInput, 100000), Dimension: 1024,
	}
	require.True(t, largeCheckpointShape(&c))
	c.Dimension = 4096
	require.False(
		t,
		largeCheckpointShape(&c),
		"a large document cannot allocate unbounded vector memory",
	)
	c.Dimension = 1024
	c.Inputs = append(c.Inputs, types.FeishuLegacyEmbeddingInput{})
	require.False(t, largeCheckpointShape(&c))
	require.False(t, largeCheckpointShape(nil))
	_, _, ok := LargeSourceReparseRequest(context.Background())
	require.False(t, ok)
	claim := &sourceDocumentReparseClaim{request: types.FeishuDocumentReparse{
		ID: "request", FileType: "xlsx", ConfigHash: strings.Repeat("1", 64), ProgressWindows: LargeFeishuProgressWindows - 1,
	}}
	ctx := context.WithValue(context.Background(), sourceDocumentReparseKey{}, claim)
	id, _, ok := LargeSourceReparseRequest(ctx)
	require.True(t, ok)
	require.Equal(t, "request", id)
	claim.request.ProgressWindows++
	_, _, ok = LargeSourceReparseRequest(ctx)
	require.False(t, ok, "successful windows also have a finite budget")
	claim.request.ProgressWindows = 0
	claim.request.FileType = "docx"
	_, _, ok = LargeSourceReparseRequest(ctx)
	require.False(t, ok, "only explicit spreadsheets use the larger path")
}
