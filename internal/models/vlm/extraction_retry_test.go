package vlm

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuExtractionRetriesCompleteOutputOnce(t *testing.T) {
	calls := 0
	v := &RemoteAPIVLM{chat: extractionChat{call: func(opts *chat.ChatOptions) *types.ChatResponse {
		calls++
		require.NotNil(t, opts.Thinking)
		require.False(t, *opts.Thinking)
		if calls == 1 {
			require.Equal(t, defaultMaxToks, opts.MaxTokens)
			return &types.ChatResponse{Content: "partial", FinishReason: "length"}
		}
		require.Equal(t, extractionMaxToks, opts.MaxTokens)
		return &types.ChatResponse{Content: "complete extraction", FinishReason: "stop"}
	}}}
	ctx := feishusafety.WithScope(t.Context(), "controlled-feishu-source")
	text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.NoError(t, err)
	require.Equal(t, "complete extraction", text)
	require.Equal(t, 2, calls)
}

func TestFeishuExtractionRetryRemainsBounded(t *testing.T) {
	calls := 0
	v := &RemoteAPIVLM{chat: extractionChat{call: func(opts *chat.ChatOptions) *types.ChatResponse {
		calls++
		require.LessOrEqual(t, opts.MaxTokens, extractionMaxToks)
		return &types.ChatResponse{Content: "still partial", FinishReason: "length"}
	}}}
	ctx := feishusafety.WithScope(t.Context(), "controlled-feishu-source")
	text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.ErrorContains(t, err, "truncated at 20000 tokens")
	require.Empty(t, text)
	require.Equal(t, 2, calls)
	calls = 0
	text, err = v.Predict(t.Context(), [][]byte{testPNG}, "extract")
	require.NoError(t, err)
	require.Equal(t, "still partial", text)
	require.Equal(t, 1, calls, "interactive VLM behavior remains unchanged")
}
