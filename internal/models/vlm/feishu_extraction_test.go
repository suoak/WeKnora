package vlm

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type extractionChatBase = chat.Chat

type extractionChat struct {
	extractionChatBase
	call func(*chat.ChatOptions) *types.ChatResponse
}

func (c extractionChat) Chat(_ context.Context, _ []chat.Message,
	opts *chat.ChatOptions,
) (*types.ChatResponse, error) {
	return c.call(opts), nil
}

func TestFeishuVLMExtractionDisablesReasoningWithoutChangingOnline(t *testing.T) {
	var scoped bool
	v := &RemoteAPIVLM{chat: extractionChat{call: func(opts *chat.ChatOptions) *types.ChatResponse {
		require.Equal(t, defaultMaxToks, opts.MaxTokens)
		if !scoped {
			require.Nil(t, opts.Thinking, "ordinary VLM calls retain model defaults")
			return &types.ChatResponse{Content: "", FinishReason: "length"}
		}
		require.NotNil(t, opts.Thinking)
		require.False(t, *opts.Thinking)
		return &types.ChatResponse{Content: "verified image text", FinishReason: "stop"}
	}}}
	_, err := v.Predict(t.Context(), [][]byte{testPNG}, "extract")
	require.ErrorContains(t, err, "truncated")
	scoped = true
	ctx := feishusafety.WithScope(t.Context(), "controlled-feishu-source")
	text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.NoError(t, err)
	require.Equal(t, "verified image text", text)
	scoped = false
	_, err = v.Predict(t.Context(), [][]byte{testPNG}, "extract")
	require.ErrorContains(t, err, "truncated")
}

func TestFeishuVLMRejectsNonemptyTruncatedExtraction(t *testing.T) {
	v := &RemoteAPIVLM{chat: extractionChat{call: func(*chat.ChatOptions) *types.ChatResponse {
		return &types.ChatResponse{Content: "only half of the image text", FinishReason: "length"}
	}}}
	ctx := feishusafety.WithScope(t.Context(), "controlled-feishu-source")
	text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.ErrorContains(t, err, "truncated")
	require.Empty(t, text, "an incomplete extraction must never reach publication")
	text, err = v.Predict(t.Context(), [][]byte{testPNG}, "extract")
	require.NoError(t, err, "non-Feishu VLM compatibility is preserved")
	require.NotEmpty(t, text)
}

func TestFeishuVLMExtractionUsesExistingGatewayThinkingDialect(t *testing.T) {
	withVLMSSRFWhitelist(t, "127.0.0.1")
	var body map[string]interface{}
	server := newVLMChatTestServer(t, &body)
	defer server.Close()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "vlm-extraction-wire"
	guard, err := feishusafety.New(client, policy)
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), guard.PermitKey(), guard.Fingerprint(), time.Minute).Err())
	previous := feishusafety.Default()
	feishusafety.InstallDefault(guard)
	t.Cleanup(func() { feishusafety.InstallDefault(previous) })
	v, err := NewRemoteAPIVLM(&Config{
		BaseURL: server.URL, ModelName: "deepseek-v4.1-flash", APIKey: "test-only",
		Provider: "generic", Extra: map[string]interface{}{"thinking_control": "chat_template_kwargs"},
	})
	require.NoError(t, err)
	ctx := feishusafety.WithScope(t.Context(), "controlled-feishu-source")
	text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.NoError(t, err)
	require.Equal(t, "extracted text", text)
	template, ok := body["chat_template_kwargs"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, template["enable_thinking"])
	require.Equal(t, float64(defaultMaxToks), body["max_tokens"])
	// The same wire path must still close when its independent permit vanishes.
	require.NoError(t, client.Del(t.Context(), guard.PermitKey()).Err())
	_, err = v.Predict(ctx, [][]byte{testPNG}, "extract")
	require.ErrorIs(t, err, feishusafety.ErrClosed)
}
