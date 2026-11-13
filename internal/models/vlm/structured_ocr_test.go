package vlm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type structuredOCRChat struct {
	extractionChatBase
	call func([]chat.Message, *chat.ChatOptions) *types.ChatResponse
}

func (c structuredOCRChat) Chat(_ context.Context, messages []chat.Message,
	opts *chat.ChatOptions,
) (*types.ChatResponse, error) {
	return c.call(messages, opts), nil
}

func TestFeishuStructuredOCRScopeAndEvidence(t *testing.T) {
	scoped := feishusafety.WithScope(t.Context(), "structured-ocr-source")
	for _, tc := range []struct {
		name       string
		ctx        context.Context
		capable    bool
		structured bool
	}{
		{"feishu_ocr", WithOCRExtraction(scoped), true, true},
		{"complete_ocr", WithOCRExtraction(scoped), true, false},
		{"feishu_caption", scoped, true, false},
		{"interactive_ocr", WithOCRExtraction(t.Context()), true, false},
		{"unsupported_protocol", WithOCRExtraction(scoped), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			v := &RemoteAPIVLM{structuredOCR: tc.capable, chat: structuredOCRChat{
				call: func(messages []chat.Message, opts *chat.ChatOptions) *types.ChatResponse {
					calls++
					parts := messages[0].MultiContent
					require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(testPNG), parts[1].ImageURL.URL)
					if calls == 1 && (tc.structured || tc.name == "unsupported_protocol") {
						require.Empty(t, opts.Format)
						require.Equal(t, "original prompt", parts[0].Text)
						return &types.ChatResponse{Content: "discarded partial", FinishReason: "length"}
					}
					if tc.structured {
						require.Equal(t, extractionMaxToks, opts.MaxTokens)
						require.JSONEq(t, string(structuredOCRSchema), string(opts.Format))
						require.Equal(t, structuredOCRPrompt, parts[0].Text)
						return &types.ChatResponse{Content: `{"lines":["prompt$","prompt$","2023_07_12 2024_02_22"]}`, FinishReason: "stop"}
					}
					require.Empty(t, opts.Format)
					require.Equal(t, "original prompt", parts[0].Text)
					return &types.ChatResponse{Content: "original text", FinishReason: "stop"}
				},
			}}
			text, err := v.Predict(tc.ctx, [][]byte{testPNG}, "original prompt")
			require.NoError(t, err)
			if tc.structured {
				require.Equal(t, "prompt$\nprompt$\n2023_07_12 2024_02_22", text)
			} else {
				require.Equal(t, "original text", text)
			}
		})
	}
}

func TestFeishuStructuredOCRRejectsIncompleteOutput(t *testing.T) {
	ctx := WithOCRExtraction(feishusafety.WithScope(t.Context(), "structured-ocr-source"))
	for _, finish := range []string{"length", "content_filter", ""} {
		t.Run(finish, func(t *testing.T) {
			calls := 0
			v := &RemoteAPIVLM{structuredOCR: true, chat: structuredOCRChat{
				call: func(_ []chat.Message, opts *chat.ChatOptions) *types.ChatResponse {
					calls++
					if calls == 1 {
						require.Empty(t, opts.Format)
						return &types.ChatResponse{Content: "discarded partial", FinishReason: "length"}
					}
					require.NotEmpty(t, opts.Format)
					return &types.ChatResponse{Content: `{"lines":["apparently complete"]}`, FinishReason: finish}
				},
			}}
			text, err := v.Predict(ctx, [][]byte{testPNG}, "extract")
			require.Error(t, err)
			require.Empty(t, text)
			require.Equal(t, 2, calls, "the existing complete retry stays bounded")
		})
	}
}

func TestFeishuStructuredOCRClosedObject(t *testing.T) {
	for _, input := range []string{
		`{}`, `{"lines":null}`, `{"lines":"text"}`, `{"lines":[1]}`, `{"lines":[null]}`,
		`{"lines":[],"extra":"ignored"}`, `{"lines":[],"lines":["replacement"]}`,
		`{"lines":[]} {"lines":["second object"]}`, `{"lines":["half"`,
		"```json\n{\"lines\":[]}\n```", string([]byte{0xff}), strings.Repeat(" ", (1<<20)+1),
	} {
		text, err := decodeStructuredOCR(input)
		require.Error(t, err)
		require.Empty(t, text)
	}
	text, err := decodeStructuredOCR(`{"lines":[]}`)
	require.NoError(t, err)
	require.Empty(t, text, "an explicit empty array represents an image without text")
}

func TestFeishuStructuredOCRWire(t *testing.T) {
	withVLMSSRFWhitelist(t, "127.0.0.1")
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "structured-ocr-wire"
	guard, err := feishusafety.New(client, policy)
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), guard.PermitKey(), guard.Fingerprint(), time.Minute).Err())
	previous := feishusafety.Default()
	feishusafety.InstallDefault(guard)
	t.Cleanup(func() { feishusafety.InstallDefault(previous) })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			require.NotContains(t, body, "response_format")
			require.Equal(t, float64(defaultMaxToks), body["max_tokens"])
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"discarded partial"},"finish_reason":"length"}]}`))
			return
		}
		require.Equal(t, map[string]interface{}{"type": "json_object"}, body["response_format"])
		require.Equal(t, map[string]interface{}{"type": "disabled"}, body["thinking"])
		require.Equal(t, float64(extractionMaxToks), body["max_tokens"])
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"lines\":[\"one\",\"one\",\"two\"]}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	v, err := NewRemoteAPIVLM(&Config{
		BaseURL: server.URL, ModelName: "deepseek-v4.1-flash", APIKey: "test-only",
		Provider: "generic", Extra: map[string]interface{}{"thinking_control": "thinking_type"},
	})
	require.NoError(t, err)
	text, err := v.Predict(WithOCRExtraction(feishusafety.WithScope(t.Context(), "structured-ocr-source")),
		[][]byte{testPNG}, "extract")
	require.NoError(t, err)
	require.Equal(t, "one\none\ntwo", text)
	require.Equal(t, int32(2), calls.Load())
}
