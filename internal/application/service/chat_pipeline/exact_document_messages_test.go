package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExactDocumentMessageReconstructionPreservesVerifiedRows(t *testing.T) {
	ctx := context.Background()
	body := strings.Repeat("Verified original document. ", 50)
	request := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "Summarize", RerankTopK: 10,
			SummaryConfig: types.SummaryConfig{
				Prompt:          "Answer from {{contexts}}",
				ContextTemplate: "{{contexts}} {{query}}",
			},
		},
		PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{{
			ID:              "5335a844-ae6d-449e-a38a-e5bc5733e4fd",
			KnowledgeID:     "ab6db799-26a3-47d4-8dab-1c31a655f101",
			KnowledgeBaseID: "9fc77590-d7c9-4c77-baa1-332640970e78", Content: body,
			KnowledgeTitle: "Original", ChunkType: string(types.ChunkTypeText), Score: 1,
		}}},
	}
	expected, err := RenderVerifiedDocumentMessages(ctx, request, nil)
	require.NoError(t, err)
	require.Len(t, expected, 2)
	require.Contains(t, expected[1].Content, "Verified original document.")
	require.Empty(t, request.UserContent)
	require.Empty(t, request.MergeResult)
	next := func() *PluginError { return nil }
	require.Nil(t, (&PluginMerge{}).OnEvent(ctx, types.CHUNK_MERGE, request, next))
	require.Nil(t, (&PluginIntoChatMessage{}).OnEvent(ctx, types.INTO_CHAT_MESSAGE, request, next))
	messages, registry := prepareMessagesWithModelContext(ctx, request)
	actual := registry.EncodeMessages(messages)
	require.Equal(t, expected, actual)
	require.Equal(t, []chat.Message{
		{Role: "system", Content: actual[0].Content},
		{Role: "user", Content: actual[1].Content},
	}, actual)
}
