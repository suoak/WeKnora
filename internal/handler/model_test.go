package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type debugBatchModelService struct {
	interfaces.ModelService
	embedder   embedding.Embedder
	embedderID string
}

type effectivePolicyModelService struct{ interfaces.ModelService }

func (*effectivePolicyModelService) GetModelByID(context.Context, string) (*types.Model, error) {
	return &types.Model{
		ID: "builtin-chat", Name: "provider-name", DisplayName: "Safe name",
		Type: types.ModelTypeKnowledgeQA, IsBuiltin: true, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{
			BaseURL: "https://secret.example", APIKey: "secret-api-key",
			AppSecret: "secret-app-value", CustomHeaders: map[string]string{"Authorization": "secret-header"},
		},
	}, nil
}

type effectivePolicyStub struct{ interfaces.ModelPolicyService }

func (*effectivePolicyStub) ResolveChatModelID(context.Context) string      { return "builtin-chat" }
func (*effectivePolicyStub) ResolveSummaryModelID(context.Context) string   { return "" }
func (*effectivePolicyStub) ResolveEmbeddingModelID(context.Context) string { return "" }
func (*effectivePolicyStub) ResolveRerankModelID(context.Context) string    { return "" }
func (*effectivePolicyStub) ResolveVLMModelID(context.Context) string       { return "" }
func (*effectivePolicyStub) ResolveASRModelID(context.Context) string       { return "" }

func (s *debugBatchModelService) GetEmbeddingModel(_ context.Context, id string) (embedding.Embedder, error) {
	s.embedderID = id
	return s.embedder, nil
}

type debugBatchEmbedder struct {
	embedding.Embedder
	vectors     [][]float32
	texts       []string
	passedModel embedding.Embedder
}

func (e *debugBatchEmbedder) BatchEmbedWithPool(
	_ context.Context, model embedding.Embedder, texts []string,
) ([][]float32, error) {
	e.passedModel = model
	e.texts = append([]string(nil), texts...)
	return e.vectors, nil
}

func runDebugEmbeddingsHandler(t *testing.T, service interfaces.ModelService, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/models/model-1/debug/embeddings", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "model-1"}}
	NewModelHandler(service, nil).DebugEmbeddings(ctx)
	return recorder, ctx
}

func TestGetDefaultPolicyReturnsCredentialFreeProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/models/default-policy", nil)
	NewModelHandler(&effectivePolicyModelService{}, &effectivePolicyStub{}).GetDefaultPolicy(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{
		"chat":{"id":"builtin-chat","name":"Safe name","type":"KnowledgeQA"},
		"summary":null,"embedding":null,"rerank":null,"vlm":null,"asr":null
	}`, recorder.Body.String())
	for _, secret := range []string{"secret.example", "secret-api-key", "secret-app-value", "secret-header", "parameters", "base_url", "api_key"} {
		assert.NotContains(t, recorder.Body.String(), secret)
	}
}

func TestModelUpdateRequestDisplayNamePresence(t *testing.T) {
	var omitted UpdateModelRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"gpt-4o"}`), &omitted))
	assert.Nil(t, omitted.DisplayName)

	var cleared UpdateModelRequest
	require.NoError(t, json.Unmarshal([]byte(`{"display_name":""}`), &cleared))
	require.NotNil(t, cleared.DisplayName)
	assert.Equal(t, "", *cleared.DisplayName)
}

func TestParseModelDebugOptionsPreservesExplicitThinkingFalse(t *testing.T) {
	opts, err := parseModelDebugOptions(`{"thinking":false,"temperature":0,"max_tokens":256}`)
	require.NoError(t, err)
	require.NotNil(t, opts.Thinking)
	assert.False(t, *opts.Thinking)
	require.NotNil(t, opts.Temperature)
	assert.Zero(t, *opts.Temperature)
	require.NotNil(t, opts.MaxTokens)
	assert.Equal(t, 256, *opts.MaxTokens)
}

func TestParseModelDebugOptionsRejectsOutOfRangeValues(t *testing.T) {
	_, err := parseModelDebugOptions(`{"top_p":0}`)
	require.ErrorContains(t, err, "top_p")
}

func TestRedactedDebugConfig(t *testing.T) {
	got := redactedDebugConfig(map[string]string{
		"thinking_control": "enable_thinking",
		"secret_key":       "do-not-leak",
		"access_token":     "do-not-leak-either",
	})
	assert.Equal(t, "enable_thinking", got["thinking_control"])
	assert.Equal(t, "[REDACTED]", got["secret_key"])
	assert.Equal(t, "[REDACTED]", got["access_token"])
}

func TestConsumeModelDebugChatStream(t *testing.T) {
	stream := make(chan types.StreamResponse, 5)
	stream <- types.StreamResponse{ResponseType: types.ResponseTypeThinking, Content: "reason "}
	stream <- types.StreamResponse{ResponseType: types.ResponseTypeThinking, Content: "more", Done: true}
	stream <- types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: "answer "}
	stream <- types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: "done"}
	stream <- types.StreamResponse{
		ResponseType: types.ResponseTypeAnswer,
		Done:         true,
		FinishReason: "stop",
		Usage:        &types.TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}
	close(stream)

	got, err := consumeModelDebugChatStream(stream)
	require.NoError(t, err)
	assert.Equal(t, "reason more", got.ReasoningContent)
	assert.Equal(t, "answer done", got.Content)
	assert.Equal(t, "stop", got.FinishReason)
	require.NotNil(t, got.Usage)
	assert.Equal(t, 7, got.Usage.TotalTokens)
	assert.Len(t, got.StreamEvents, 5)
}

func TestDebugEmbeddingsUsesSavedModelBatchPathWithoutEchoingInputs(t *testing.T) {
	embedder := &debugBatchEmbedder{vectors: [][]float32{{1, 2, 3}, {4, 5, 6}}}
	service := &debugBatchModelService{
		embedder: embedder,
	}
	body := []byte(`{"texts":["secret-input-marker","second"]}`)
	recorder, ctx := runDebugEmbeddingsHandler(t, service, body)

	require.Empty(t, ctx.Errors)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "model-1", service.embedderID)
	assert.Equal(t, []string{"secret-input-marker", "second"}, embedder.texts)
	assert.Same(t, embedder, embedder.passedModel)
	assert.NotContains(t, recorder.Body.String(), "secret-input-marker")
	assert.NotContains(t, recorder.Body.String(), "api_key")
	assert.NotContains(t, recorder.Body.String(), "base_url")
	assert.JSONEq(t, `{"success":true,"data":{"model_id":"model-1","count":2,"dimension":3,"vectors":[[1,2,3],[4,5,6]]}}`, recorder.Body.String())
}

func TestDebugEmbeddingsValidatesRequestBounds(t *testing.T) {
	service := &debugBatchModelService{}
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"empty", []byte(`{"texts":[]}`), "texts must not be empty"},
		{"blank item", []byte(`{"texts":["  "]}`), "each text must not be empty"},
		{"model configuration override", []byte(`{"texts":["safe"],"api_key":"must-not-be-accepted"}`), "invalid request body"},
		{"trailing JSON", []byte(`{"texts":["safe"]}{}`), "invalid request body"},
		{"too many", []byte(`{"texts":["x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x","x"]}`), "texts cannot exceed 32 items"},
		{"too large", []byte(`{"texts":["` + strings.Repeat("x", modelDebugMaxInputBytes) + `"]}`), "request body exceeds 64 KiB"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, ctx := runDebugEmbeddingsHandler(t, service, test.body)
			require.Len(t, ctx.Errors, 1)
			assert.ErrorContains(t, ctx.Errors.Last().Err, test.want)
		})
	}
}

func TestDebugEmbeddingsRejectsCountAndDimensionMismatch(t *testing.T) {
	tests := []struct {
		name    string
		vectors [][]float32
		want    string
	}{
		{"count", [][]float32{{1, 2}}, "count mismatch"},
		{"dimension", [][]float32{{1, 2}, {3}}, "dimension mismatch"},
		{"empty vector", [][]float32{{}, {}}, "dimension mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &debugBatchModelService{
				embedder: &debugBatchEmbedder{vectors: test.vectors},
			}
			_, ctx := runDebugEmbeddingsHandler(t, service, []byte(`{"texts":["one","two"]}`))
			require.Len(t, ctx.Errors, 1)
			assert.ErrorContains(t, ctx.Errors.Last().Err, test.want)
		})
	}
}
