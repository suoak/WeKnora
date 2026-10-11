package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/googlegenai"
	"github.com/Tencent/WeKnora/internal/models/api/openaicompletions"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sink is the real engine persistence boundary; only storage is recorded.
func TestA5HTTPCompactionActualCheckpointSink(t *testing.T) {
	for _, failures := range []int32{0, 1, 4} {
		t.Run(fmt.Sprintf("failures=%d", failures), func(t *testing.T) {
			t.Setenv("SSRF_WHITELIST", "127.0.0.1")
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if calls.Add(1) <= failures {
					fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"INCOMPLETE_SUMMARY"}}]}`+"\n\n")
				} else {
					fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"COMPLETE_SUMMARY"},"finish_reason":"stop"}]}`+"\n\n")
				}
			}))
			defer srv.Close()
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "summary-test", Client: srv.Client()}})
			engine := newTestEngine(t, model, withMaxContextTokens(40000), withMaxCompletionTokens(4000))
			sink := &recordingCheckpointSink{}
			engine.SetContextCheckpointSink(sink)
			tool := newCountingTool("write_sandbox_file")
			engine.toolRegistry = agenttools.NewToolRegistry()
			engine.toolRegistry.RegisterTool(tool)
			messages := storedHistoryOverflow()
			compacted, changed := engine.manageContextWindow(context.Background(), messages, 1, engine.tokenEstimator.EstimateMessages(messages))
			require.True(t, changed)
			require.Equal(t, []string{"turn-b"}, sink.turnIDs)
			require.Len(t, sink.saved, 1)
			assert.NotContains(t, sink.saved[0].Summary, "INCOMPLETE_SUMMARY")
			assert.Equal(t, failures == 4, sink.saved[0].Degraded)
			assert.False(t, sink.saved[0].CreatedAt.IsZero())
			if failures == 4 {
				assert.Contains(t, sink.saved[0].Summary, "Raw conversation archive")
			} else {
				assert.Contains(t, sink.saved[0].Summary, "COMPLETE_SUMMARY")
			}
			wantCalls := int32(2)
			if failures == 1 {
				wantCalls = 3
			}
			if failures == 4 {
				wantCalls = 4
			}
			assert.Equal(t, wantCalls, calls.Load())
			engine.manageContextWindow(context.Background(), compacted, 2, engine.tokenEstimator.EstimateMessages(compacted))
			assert.Len(t, sink.saved, 1, "no duplicate checkpoint in the next round")
			assert.Equal(t, wantCalls, calls.Load(), "no repeat summarization in the next round")
			assert.Zero(t, tool.calls, "summary retry must not replay historical tools")
		})
	}
}

type a5LogCapture struct {
	mu   sync.Mutex
	body strings.Builder
}

func (b *a5LogCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.Write(p)
}
func (b *a5LogCapture) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.body.String() }

// This assertion intentionally remains a real FAIL if the existing shared
// checkpoint logger leaks driver details. A5 may not repair that shared file.
func TestA5CheckpointSinkErrorMustNotLeakCanaries(t *testing.T) {
	canaries := []string{"A5_SYNTHETIC_SQL_CANARY", "A5_SYNTHETIC_PROVIDER_CANARY", "A5_SYNTHETIC_TOKEN_CANARY", "Bearer A5_SYNTHETIC_BEARER_CANARY"}
	capture := &a5LogCapture{}
	logger.SetOutput(io.MultiWriter(os.Stderr, capture))
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })
	engine := newTestEngine(t, &summarizerChat{}, withMaxContextTokens(40000), withMaxCompletionTokens(4000))
	sink := &recordingCheckpointSink{err: errors.New(strings.Join(canaries, " "))}
	engine.SetContextCheckpointSink(sink)
	messages := storedHistoryOverflow()
	compacted, changed := engine.manageContextWindow(context.Background(), messages, 1, engine.tokenEstimator.EstimateMessages(messages))
	require.True(t, changed)
	require.Len(t, sink.saved, 1)
	assert.Less(t, engine.tokenEstimator.EstimateMessages(compacted), engine.tokenEstimator.EstimateMessages(messages))
	assert.Contains(t, capture.String(), "Failed to save context checkpoint")
	assert.Contains(t, capture.String(), "checkpoint_save_failed")
	assert.NotContains(t, capture.String(), "Saved context checkpoint")
	assert.False(t, sink.saved[0].Degraded)
	assert.Equal(t, []string{"turn-b"}, sink.turnIDs)
	for _, canary := range canaries {
		assert.NotContains(t, capture.String(), canary, "shared sink errors must be redacted")
	}
}

type countingTool struct {
	agenttools.BaseTool
	calls int
}

func newCountingTool(name string) *countingTool {
	return &countingTool{BaseTool: agenttools.NewBaseTool(name, "test", json.RawMessage(`{"type":"object"}`))}
}

func (t *countingTool) Execute(context.Context, json.RawMessage) (*types.ToolResult, error) {
	t.calls++
	return &types.ToolResult{Success: true, Output: "executed"}, nil
}

// ---------------------------------------------------------------------------
// Mock: chat.Chat
// ---------------------------------------------------------------------------

type mockResponse struct {
	chunks []types.StreamResponse
}

type mockChat struct {
	mu        sync.Mutex
	responses []mockResponse
	calls     [][]chat.Message
	opts      []*chat.ChatOptions
	callCount int
}

func (m *mockChat) ChatStream(
	_ context.Context,
	messages []chat.Message,
	opts *chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("unexpected ChatStream call #%d (only %d responses prepared)", m.callCount, len(m.responses))
	}
	resp := m.responses[m.callCount]
	m.calls = append(m.calls, append([]chat.Message(nil), messages...))
	m.opts = append(m.opts, opts)
	m.callCount++

	ch := make(chan types.StreamResponse, len(resp.chunks))
	for _, chunk := range resp.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestStreamLLMResourceAliasesRoundTrip(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "![image](res://0"},
		{ResponseType: types.ResponseTypeAnswer, Content: "001)", Done: true},
	}}}}
	engine := newTestEngine(t, model)
	result, err := engine.streamLLMToEventBus(
		context.Background(),
		[]chat.Message{{Role: "tool", Content: "source=" + ref}},
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "![image]("+ref+")", result.Content)
	require.Len(t, model.calls, 1)
	require.Equal(t, "source=res://0001", model.calls[0][0].Content)
}

// TestStreamLLMSummarySlugSurvivesDocumentCompaction is the regression guard for
// the mangled `summary/<uuid>` → `summary/d1` bug. A wiki summary-page slug
// embeds a document's UUID. The unified model-context registry owns the
// resource-before-source encoding order so the slug cannot become summary/d1.
func TestStreamLLMSummarySlugSurvivesDocumentCompaction(t *testing.T) {
	const knowledgeID = "07a20bb1-a662-47cf-9929-06fb5d5b5b5e"
	const summarySlug = "summary/" + knowledgeID

	// The model copies the protected token it saw back into a wiki_read call.
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{
			ResponseType: types.ResponseTypeAnswer,
			Content:      "reading the summary",
			ToolCalls: []types.LLMToolCall{{
				Type: "function",
				Function: types.FunctionCall{
					Name:      "wiki_read_page",
					Arguments: `{"slugs":["res://0001"]}`,
				},
			}},
			Done:         true,
			FinishReason: "tool_calls",
		},
	}}}}

	engine := newTestEngine(t, model)
	// The document UUID is registered as citation alias d1, exactly as the RAG
	// context (<document id="d1">…) would have registered it upstream.
	require.Equal(t, "d1", engine.modelContext.RegisterDocument(knowledgeID))

	toolMsg := chat.Message{
		Role:    "tool",
		Content: `<link>[[` + summarySlug + `|Weknora 试错记录.md - Summary]]</link>`,
	}
	result, err := engine.streamLLMToEventBus(context.Background(),
		[]chat.Message{toolMsg}, nil, nil)
	require.NoError(t, err)

	// What the model actually saw must NOT contain the mangled slug; the UUID
	// must have been aliased to a res:// token before citation compaction ran.
	require.Len(t, model.calls, 1)
	sent := model.calls[0][0].Content
	require.NotContains(t, sent, "summary/d1",
		"summary slug was clobbered by document-id compaction (encode ordering regressed)")
	require.Contains(t, sent, "res://", "summary slug must be protected as a res:// token")

	// The model's tool call echoing the token must decode back to the real slug.
	require.Len(t, result.ToolCalls, 1)
	require.Contains(t, result.ToolCalls[0].Function.Arguments, summarySlug)
	require.NotContains(t, result.ToolCalls[0].Function.Arguments, "res://")
}

// Reproduce an MCP-only turn following a property-management answer, with
// unrelated FAQ entries injected by the bound-KB directory.
func TestStreamMCPAnswerRejectsUnretrievedKnowledgeCitations(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: `KM文章<ref id="c`},
		{ResponseType: types.ResponseTypeAnswer, Content: `3"/><ref id="c1"/><ref id="c2"/> 正确来源<ref id="w`},
		{ResponseType: types.ResponseTypeAnswer, Content: `1"/>`, Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	engine.knowledgeBasesInfo = []*KnowledgeBaseInfo{{
		ID: "faq-kb", Name: "FAQ TEST", Type: "faq", RecentDocs: []RecentDocInfo{
			{ChunkID: "faq-1", Title: "什么是 WeKnora？", FAQStandardQuestion: "什么是 WeKnora？"},
			{ChunkID: "faq-2", Title: "如何创建知识库？", FAQStandardQuestion: "如何创建知识库？"},
		},
	}}
	userTurn := engine.RenderUserTurnContent("session", "KM上有趣的事情")
	const article = "https://km.woa.com/articles/show/669504?jumpfrom=kmmcp"
	toolResult := engine.modelContext.ModelToolResultForTool("call_mcp_tool", &types.ToolResult{
		Success: true, Output: "标题: AI玩法\n摘要: Computer Use 案例\n链接: " + article,
	})
	var emitted strings.Builder
	result, err := engine.streamLLMToEventBus(context.Background(), []chat.Message{
		{Role: "assistant", Content: `物业工作<kb doc="9月13日周报.docx" chunk_id="weekly-report" />`},
		{Role: "user", Content: userTurn},
		{Role: "tool", Name: "call_mcp_tool", Content: toolResult},
	}, nil, func(chunk *types.StreamResponse, _ string) {
		emitted.WriteString(chunk.Content)
	})
	require.NoError(t, err)
	want := `KM文章 正确来源<web url="` + article + `" title="" />`
	require.Equal(t, want, result.Content)
	require.Equal(t, want, emitted.String(), "invalid references must not reach SSE even transiently")
	require.Contains(t, model.calls[0][2].Content, `<source id="w1"`)
	require.Contains(t, model.calls[0][1].Content, `chunk_id="c1"`, "FAQ handles remain available for retrieval")
}

// Reproduces the round that ended a 40-round conversation: the stream broke
// while serializing a large write_sandbox_file call, after a short preamble had
// already streamed. Treating that as a completed turn let the preamble stand in
// as the final answer and dropped the call, so the stream error must surface.
func TestStreamLLMToEventBus_ErrorAfterContent_IsNotASuccessfulTurn(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "非常好，我已经获取了骨架模板。"},
		{
			ResponseType: types.ResponseTypeError,
			Content:      "context deadline exceeded",
			Failure:      types.NewStreamFailure(types.StreamFailureDeadline, 0, 0),
			Done:         true,
			FinishReason: types.FinishReasonIncomplete,
			ToolCalls: []types.LLMToolCall{{
				ID: "call-1",
				Function: types.FunctionCall{
					Name:      "write_sandbox_file",
					Arguments: "{\"path\":\"/a.html\",\"content\":\"<htm",
				},
			}},
		},
	}}}}

	engine := newTestEngine(t, model)
	result, err := engine.streamLLMToEventBus(context.Background(), nil, nil, nil)

	require.Error(t, err, "a broken stream must not be reported as a completed turn")
	require.Contains(t, err.Error(), "context deadline exceeded")
	// The partial call rides along for diagnostics, and the finish reason must
	// never fall back to "stop" — that fallback is what ended the conversation.
	require.Len(t, result.ToolCalls, 1)
	require.Equal(t, types.FinishReasonIncomplete, result.FinishReason)
	require.True(t, isTransientError(err), "a broken stream must be retryable")
}

func TestStreamLLMToEventBus_MessageOnlyDeadlineIsUnknown(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "legal partial content"},
		{ResponseType: types.ResponseTypeError, Content: "context deadline exceeded overflow B2_MESSAGE_CANARY", Done: true, FinishReason: types.FinishReasonIncomplete},
	}}}}
	engine := newTestEngine(t, model)
	result, err := engine.streamLLMToEventBus(context.Background(), nil, nil, nil)
	require.Error(t, err)
	var failure *types.StreamFailure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, types.StreamFailureUnknown, failure.Code())
	require.False(t, isTransientError(err))
	require.False(t, compaction.IsOverflowError(err))
	require.NotContains(t, err.Error(), "B2_MESSAGE_CANARY")
	require.Equal(t, types.FinishReasonIncomplete, result.FinishReason)
}

func TestStreamLLMChunkReferenceExpandsBeforeEmission(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: `answer <ref id="`},
		{ResponseType: types.ResponseTypeAnswer, Content: `c1"/>`, Done: true},
	}}}}
	engine := newTestEngine(t, model)
	engine.modelContext.RegisterChunk(modelcontext.ChunkReference{
		ChunkID:         "chunk-1",
		KnowledgeBaseID: "kb-1",
		DocumentTitle:   "Doc",
	})
	result, err := engine.streamLLMToEventBus(context.Background(), nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, `answer <kb doc="Doc" chunk_id="chunk-1" kb_id="kb-1" />`, result.Content)
}

func TestRunToolCallRejectsUnresolvedHandlesBeforeExecution(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	engine.toolRegistry = agenttools.NewToolRegistry()
	tool := newCountingTool("test_unresolved")
	engine.toolRegistry.RegisterTool(tool)

	result := engine.runToolCall(
		context.Background(),
		types.LLMToolCall{
			ID: "call-1",
			Function: types.FunctionCall{
				Name:      tool.Name(),
				Arguments: `{"id":"d99"}`,
			},
			ModelArguments:     `{"id":"d99"}`,
			ArgumentResolution: modelcontext.ArgumentResolutionUnresolved,
			UnresolvedHandles:  []string{"d99"},
		},
		0, 0, 1, "session", "message",
	)
	require.Zero(t, tool.calls)
	require.NotNil(t, result.Result)
	require.False(t, result.Result.Success)
	require.Contains(t, result.Result.Error, "unresolved model handles")
}

func TestRunToolCallDecodesHandlesAfterJSONRepair(t *testing.T) {
	newEngine := func() (*AgentEngine, *countingTool) {
		engine := newTestEngine(t, &mockChat{})
		engine.toolRegistry = agenttools.NewToolRegistry()
		tool := newCountingTool(agenttools.ToolReadDocument)
		engine.toolRegistry.RegisterTool(tool)
		return engine, tool
	}

	unknownEngine, unknownTool := newEngine()
	unknown := unknownEngine.runToolCall(
		context.Background(),
		types.LLMToolCall{
			ID:             "call-unknown",
			Function:       types.FunctionCall{Name: unknownTool.Name(), Arguments: `{"id":"d99",}`},
			ModelArguments: `{"id":"d99",}`,
		},
		0, 0, 1, "session", "message",
	)
	require.Zero(t, unknownTool.calls)
	require.False(t, unknown.Result.Success)
	require.Contains(t, unknown.Result.Error, "unresolved model handles")

	knownEngine, knownTool := newEngine()
	knownEngine.modelContext.RegisterDocument("doc-real")
	known := knownEngine.runToolCall(
		context.Background(),
		types.LLMToolCall{
			ID:             "call-known",
			Function:       types.FunctionCall{Name: knownTool.Name(), Arguments: `{"id":"d1",}`},
			ModelArguments: `{"id":"d1",}`,
		},
		0, 0, 1, "session", "message",
	)
	require.Equal(t, 1, knownTool.calls)
	require.True(t, known.Result.Success)
	require.Equal(t, "doc-real", known.Args["id"])
}

func (m *mockChat) Chat(_ context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockChat) GetModelName() string { return "mock-model" }
func (m *mockChat) GetModelID() string   { return "mock-id" }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type testEngineOption func(*types.AgentConfig)

func withMaxIterations(n int) testEngineOption {
	return func(cfg *types.AgentConfig) {
		cfg.MaxIterations = n
	}
}

func withCitationsEnabled(enabled bool) testEngineOption {
	return func(cfg *types.AgentConfig) {
		cfg.CitationEnabled = &enabled
	}
}

func withMaxCompletionTokens(n int) testEngineOption {
	return func(cfg *types.AgentConfig) {
		cfg.MaxCompletionTokens = n
	}
}

func withMaxContextTokens(n int) testEngineOption {
	return func(cfg *types.AgentConfig) {
		cfg.MaxContextTokens = n
	}
}

func TestWithinIterationBudgetUnlimited(t *testing.T) {
	unlimited := newTestEngine(t, &mockChat{}, withMaxIterations(types.UnlimitedMaxIterations))
	require.True(t, unlimited.withinIterationBudget(0))
	require.True(t, unlimited.withinIterationBudget(10_000))
	require.Equal(t, "unlimited", unlimited.maxIterationsDisplay())

	capped := newTestEngine(t, &mockChat{}, withMaxIterations(3))
	require.True(t, capped.withinIterationBudget(0))
	require.True(t, capped.withinIterationBudget(2))
	require.False(t, capped.withinIterationBudget(3))
	require.Equal(t, "3", capped.maxIterationsDisplay())
}

// History may not fill the window: the reply has to land somewhere. A reserve
// expressed as a fraction of the window gets this wrong in both directions —
// too little room on a small window, needlessly early compaction on a big one.
func TestContextCompactionThresholdReservesRoomForTheReply(t *testing.T) {
	engine := newTestEngine(t, &mockChat{},
		withMaxContextTokens(128000), withMaxCompletionTokens(24576))

	// Reserve is the round's own output budget plus estimation slack.
	require.Equal(t, 24576+contextSafetyTokens, engine.contextReserveTokens())
	require.Equal(t, 128000-24576-contextSafetyTokens, engine.compactor.Settings().Threshold())

	// A tiny completion budget still keeps a floor of headroom, rather than
	// letting history run to the very edge of the window.
	small := newTestEngine(t, &mockChat{},
		withMaxContextTokens(128000), withMaxCompletionTokens(1024))
	require.Equal(t, compaction.DefaultReserveTokens, small.contextReserveTokens())

	// An unknown window disables compaction rather than guessing.
	require.Nil(t, newTestEngine(t, &mockChat{}).compactor)
	require.Zero(t, newTestEngine(t, &mockChat{}).compactor.Settings().Threshold())
}

// The usage baseline already includes the assistant reply as its `output`
// half. Starting the delta at that reply counts every completion twice, and
// since the engine re-anchors on fresh usage each round the error rides along
// permanently — inflating the estimate enough to trigger compaction on a
// context that is nowhere near the threshold.
func TestEstimateCurrentTokensDoesNotDoubleCountTheReply(t *testing.T) {
	engine := newTestEngine(t, &mockChat{}, withMaxContextTokens(128000))

	sent := []chat.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "do the thing"},
	}
	reply := chat.Message{
		Role:             "assistant",
		Content:          "working on it",
		ReasoningContent: strings.Repeat("deliberating carefully. ", 200),
	}
	toolResult := chat.Message{Role: "tool", Name: "t", ToolCallID: "c1", Content: "result"}
	messages := append(append([]chat.Message{}, sent...), reply, toolResult)

	// The provider reported this round: 5000 in, and the reply as output.
	replyTokens := engine.tokenEstimator.EstimateMessage(&reply)
	engine.lastSentMsgCount = len(sent)
	engine.lastUsage = types.TokenUsage{
		PromptTokens:     5000,
		CompletionTokens: replyTokens,
		TotalTokens:      5000 + replyTokens,
	}

	got := engine.estimateCurrentTokens(messages)
	want := 5000 + replyTokens + engine.tokenEstimator.EstimateMessages(messages[len(sent)+1:])
	require.Equal(t, want, got)

	// Stated as the property that actually matters: the reply is counted once.
	require.Less(t, got, 5000+2*replyTokens,
		"the assistant reply must not be billed by both the usage baseline and the delta")
}

// The compaction trigger counts conversation only. Tool schemas ride with
// every request and show up in the provider's usage, but they are not added
// to a no-usage estimate — doing so made a 12k chat with 232 MCP tools look
// like 117k and compact every round, including the first.
func TestEstimateCurrentTokensDoesNotCountToolSchemasWithoutUsage(t *testing.T) {
	engine := newTestEngine(t, &mockChat{}, withMaxContextTokens(128000))

	messages := []chat.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "do the thing"},
	}
	tools := make([]chat.Tool, 80)
	for i := range tools {
		tools[i] = chat.Tool{
			Type: "function",
			Function: chat.FunctionDef{
				Name:        fmt.Sprintf("tool_%d", i),
				Description: strings.Repeat("does something useful. ", 80),
				Parameters:  []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		}
	}

	got := engine.estimateCurrentTokens(messages)
	require.Equal(t, engine.tokenEstimator.EstimateMessages(messages), got)

	schemaTokens := engine.tokenEstimator.EstimateTools(tools)
	require.Greater(t, schemaTokens, got*50,
		"the fixture has to dwarf the conversation, the way a large MCP tool list does")
	require.False(t, engine.compactor.Settings().ShouldCompact(got),
		"a two-message conversation must not cross the threshold")
}

// summarizerChat counts summarization calls so a test can prove the engine is
// not paying for one every round.
type summarizerChat struct {
	mockChat
	calls int
}

func (s *summarizerChat) Chat(
	context.Context, []chat.Message, *chat.ChatOptions,
) (*types.ChatResponse, error) {
	s.calls++
	return &types.ChatResponse{Content: "## Goal\ndo the thing", FinishReason: "stop"}, nil
}

// ChatStream is how compaction calls the summarizer.
func (s *summarizerChat) ChatStream(
	ctx context.Context, messages []chat.Message, opts *chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	resp, _ := s.Chat(ctx, messages, opts)
	ch := make(chan types.StreamResponse, 1)
	ch <- types.StreamResponse{
		ResponseType: types.ResponseTypeAnswer, Content: resp.Content, Done: true, FinishReason: resp.FinishReason,
	}
	close(ch)
	return ch, nil
}

// The bug this replaces: inside one ReAct turn nothing was compactable, so
// every round crossed the threshold, spent a summarization call, and freed
// nothing. The loop is only broken if a second pass over the compacted context
// declines to call the summarizer again.
func TestContextCompactionDoesNotRunEveryRound(t *testing.T) {
	llm := &summarizerChat{}
	engine := newTestEngine(t, llm,
		withMaxContextTokens(40000), withMaxCompletionTokens(4000))

	// One user message, then many assistant/tool rounds — a ReAct turn with
	// no turn boundary anywhere in it.
	messages := []chat.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "build me a deck"},
	}
	body := strings.Repeat("tool output content ", 400)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("call-%d", i)
		messages = append(messages,
			chat.Message{Role: "assistant", ToolCalls: []chat.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: chat.FunctionCall{Name: "write_sandbox_file", Arguments: `{"path":"/w/a.html"}`},
			}}},
			chat.Message{Role: "tool", Name: "write_sandbox_file", ToolCallID: id, Content: body},
		)
	}

	before := engine.tokenEstimator.EstimateMessages(messages)
	require.True(t, engine.compactor.Settings().ShouldCompact(before))

	compacted, changed := engine.manageContextWindow(
		context.Background(), messages, 1, before,
	)
	require.True(t, changed)
	after := engine.tokenEstimator.EstimateMessages(compacted)
	require.Less(t, after, before/2, "compaction has to actually free room")
	callsAfterFirst := llm.calls
	require.Positive(t, callsAfterFirst)

	// Second round over the already-compacted context: no LLM call, because
	// there is nothing left outside the keep-recent budget.
	_, changedAgain := engine.manageContextWindow(
		context.Background(), compacted, 2, after,
	)
	require.False(t, changedAgain)
	require.Equal(t, callsAfterFirst, llm.calls,
		"a context that cannot shrink must not spend another summarization call")
}

// recordingCheckpointSink captures what the engine persists.
type recordingCheckpointSink struct {
	turnIDs []string
	saved   []*types.ContextCheckpoint
	err     error
}

func (s *recordingCheckpointSink) SaveContextCheckpoint(
	_ context.Context, turnMessageID string, checkpoint *types.ContextCheckpoint,
) error {
	s.turnIDs = append(s.turnIDs, turnMessageID)
	s.saved = append(s.saved, checkpoint)
	return s.err
}

// storedHistoryOverflow is a request whose stored history ends on turn-b and
// whose live turn has outgrown the keep-recent budget on its own.
func storedHistoryOverflow() []chat.Message {
	messages := []chat.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "first question", TurnID: "turn-a"},
		{Role: "assistant", Content: "first answer", TurnID: "turn-a"},
		{Role: "user", Content: "second question", TurnID: "turn-b"},
		{Role: "assistant", Content: "second answer", TurnID: "turn-b"},
		{Role: "user", Content: "build me a deck"},
	}
	body := strings.Repeat("tool output content ", 400)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("call-%d", i)
		messages = append(messages,
			chat.Message{Role: "assistant", ToolCalls: []chat.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: chat.FunctionCall{Name: "write_sandbox_file", Arguments: `{"path":"/w/a.html"}`},
			}}},
			chat.Message{Role: "tool", Name: "write_sandbox_file", ToolCallID: id, Content: body},
		)
	}
	return messages
}

// Without persistence every later turn re-summarizes the same stored history.
// A compaction that ends on a stored turn is written back onto that turn.
func TestContextCompactionPersistsACheckpointOnTheLastStoredTurn(t *testing.T) {
	engine := newTestEngine(t, &summarizerChat{},
		withMaxContextTokens(40000), withMaxCompletionTokens(4000))
	sink := &recordingCheckpointSink{}
	engine.SetContextCheckpointSink(sink)

	messages := storedHistoryOverflow()
	_, changed := engine.manageContextWindow(
		context.Background(), messages, 1, engine.tokenEstimator.EstimateMessages(messages),
	)
	require.True(t, changed)

	require.Equal(t, []string{"turn-b"}, sink.turnIDs)
	require.Contains(t, sink.saved[0].Summary, "do the thing")
	require.False(t, sink.saved[0].CreatedAt.IsZero())
}

// The checkpoint is an optimization for later turns. Failing to store it must
// not undo the compaction this turn needed.
func TestContextCheckpointFailureKeepsTheCompaction(t *testing.T) {
	engine := newTestEngine(t, &summarizerChat{},
		withMaxContextTokens(40000), withMaxCompletionTokens(4000))
	engine.SetContextCheckpointSink(&recordingCheckpointSink{err: errors.New("db down")})

	messages := storedHistoryOverflow()
	before := engine.tokenEstimator.EstimateMessages(messages)
	compacted, changed := engine.manageContextWindow(context.Background(), messages, 1, before)
	require.True(t, changed)
	require.Less(t, engine.tokenEstimator.EstimateMessages(compacted), before/2)
}

// A compaction can be discarded for freeing too little and still have
// summarized the stored history in full: a short stored history next to a
// live turn whose weight is one result the cut cannot reach. That summary is a
// valid checkpoint, and dropping it would have the next turn pay for it again.
func TestContextCheckpointIsKeptWhenTheCompactionFreesTooLittle(t *testing.T) {
	engine := newTestEngine(t, &summarizerChat{},
		withMaxContextTokens(40000), withMaxCompletionTokens(4000))
	sink := &recordingCheckpointSink{}
	engine.SetContextCheckpointSink(sink)

	messages := []chat.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "first question", TurnID: "turn-a"},
		{Role: "assistant", Content: "first answer", TurnID: "turn-a"},
		{Role: "user", Content: "read the export"},
		{Role: "assistant", ToolCalls: []chat.ToolCall{{
			ID: "call-1", Type: "function",
			Function: chat.FunctionCall{Name: "read_file", Arguments: `{"path":"/w/export.csv"}`},
		}}},
		{Role: "tool", Name: "read_file", ToolCallID: "call-1", Content: strings.Repeat("row,value ", 12000)},
	}
	before := engine.tokenEstimator.EstimateMessages(messages)
	require.True(t, engine.compactor.Settings().ShouldCompact(before))

	result, err := engine.compactor.Compact(context.Background(), messages, compaction.ReasonThreshold)
	require.NoError(t, err)
	require.Less(t, result.Freed(), result.TokensBefore/minFreedFraction,
		"the fixture must be a compaction the engine discards")

	engine.manageContextWindow(context.Background(), messages, 1, before)
	require.Equal(t, []string{"turn-a"}, sink.turnIDs)
}

// Redacting a stored KB result must not detach it from its turn, or a summary
// ending on that turn could no longer be recognized as ending there.
func TestRedactHistoryKBResultsKeepsTurnID(t *testing.T) {
	redacted := redactHistoryKBResults([]chat.Message{{
		Role: "tool", Name: agenttools.ToolSearchKnowledge, ToolCallID: "c1",
		Content: "stale chunk", TurnID: "turn-a",
	}})
	require.Len(t, redacted, 1)
	require.NotEqual(t, "stale chunk", redacted[0].Content)
	require.Equal(t, "turn-a", redacted[0].TurnID)
}

// History loads up to the whole window, past the compaction threshold, so the
// overflow reaches the first round's compaction instead of being dropped by
// the loader before compaction can see it.
func TestHistoryTokenBudgetExceedsTheCompactionThreshold(t *testing.T) {
	for _, tc := range []struct{ window, completion int }{
		{window: 40000, completion: 4000},
		{window: 200000, completion: 24576},
		{window: 32768, completion: 0},
	} {
		engine := newTestEngine(t, &mockChat{},
			withMaxContextTokens(tc.window), withMaxCompletionTokens(tc.completion))
		budget := HistoryTokenBudget(engine.config)
		require.Equal(t, tc.window, budget)
		require.Greater(t, budget, engine.compactor.Settings().Threshold(),
			"window=%d completion=%d", tc.window, tc.completion)
	}
	require.Equal(t, types.DefaultMaxContextTokens, HistoryTokenBudget(&types.AgentConfig{}),
		"an unresolved window falls back to the default")
}

// The history loader prices turns with HistoryAsSent, so it must match what
// buildMessagesWithLLMContext sends under both settings.
func TestHistoryAsSentFollowsTheRetainSetting(t *testing.T) {
	history := []chat.Message{
		{Role: "tool", Name: agenttools.ToolWikiReadPage, ToolCallID: "c1", Content: "full page"},
		{Role: "tool", Name: agenttools.ToolWebFetch, ToolCallID: "c2", Content: "fetched"},
	}

	redacted := HistoryAsSent(history, false)
	require.NotEqual(t, "full page", redacted[0].Content)
	require.Equal(t, "fetched", redacted[1].Content, "only KB and Wiki results are redacted")
	require.Equal(t, "full page", HistoryAsSent(history, true)[0].Content)

	engine := newTestEngine(t, &mockChat{})
	sent := engine.buildMessagesWithLLMContext("system", "next", "s1", history, nil)
	require.Equal(t, redacted[0].Content, sent[1].Content)
}

// Asking for more output than the window can still hold is rejected outright
// by the provider, which surfaces to the agent as an unexplained failure.
func TestClampCompletionBudgetToContext(t *testing.T) {
	engine := newTestEngine(t, &mockChat{},
		withMaxContextTokens(32000), withMaxCompletionTokens(24576))

	// Plenty of room: the configured budget is untouched.
	require.Equal(t, 24576, engine.clampCompletionBudgetToContext(1000))

	// Filling up: the budget shrinks to what is actually left.
	require.Equal(t, 32000-20000-contextSafetyTokens,
		engine.clampCompletionBudgetToContext(20000))

	// Past full: never returns zero or negative, which providers reject.
	require.Positive(t, engine.clampCompletionBudgetToContext(40000))

	// Unknown window means nothing to clamp against.
	require.Equal(t, 24576,
		newTestEngine(t, &mockChat{}, withMaxCompletionTokens(24576)).
			clampCompletionBudgetToContext(999999))
}

func TestBuildSystemPromptUsesInternalCitationSetting(t *testing.T) {
	model := &mockChat{}
	enabledEngine := newTestEngine(t, model)
	require.Contains(t, enabledEngine.buildSystemPrompt(context.Background()), "Source citations are enabled")

	disabledEngine := newTestEngine(t, model, withCitationsEnabled(false))
	prompt := disabledEngine.buildSystemPrompt(context.Background())
	require.Contains(t, prompt, "Source citations are disabled")
	require.NotContains(t, prompt, "Source citations are enabled")
}

func TestBuildSystemPromptUsesHostWorkspace(t *testing.T) {
	engine := newTestEngine(t, nil)
	registry := agenttools.NewToolRegistry()
	registry.RegisterTool(newCountingTool("shell_exec"))
	engine.toolRegistry = registry
	engine.SetWorkspaceLayout(sandbox.WorkspaceLayout{
		Origin: sandbox.WorkspaceOriginHost,
		Root:   "/Users/dev/My Project",
	})
	prompt := engine.buildSystemPrompt(context.Background())
	require.Contains(t, prompt, "/Users/dev/My Project")
	require.NotContains(t, prompt, "Session workspace: /workspace")
	require.NotContains(t, prompt, "There is no /workspace")
}

func newTestEngine(t *testing.T, chatModel chat.Chat, opts ...testEngineOption) *AgentEngine {
	t.Helper()
	cfg := &types.AgentConfig{
		MaxIterations: 10,
		Temperature:   0.7,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	engine := NewAgentEngine(
		cfg,
		chatModel,
		nil,
		event.NewEventBus(),
		nil,
		nil,
		"test-session",
		"",
	)
	require.NotNil(t, engine, "NewAgentEngine returned nil (agenttoken.NewEstimator failed?)")
	return engine
}

func emptyMessages() []chat.Message {
	return []chat.Message{
		{Role: "system", Content: "You are a test agent."},
		{Role: "user", Content: "test query"},
	}
}

func emptyTools() []chat.Tool {
	return nil
}

// ---------------------------------------------------------------------------
// TC1: Empty content + stop → should NOT complete with empty FinalAnswer
// ---------------------------------------------------------------------------

func TestExecuteLoop_EmptyContentWithStop_ShouldNotCompleteWithEmpty(t *testing.T) {
	// Simulate: LLM returns empty content with no tool calls (natural stop).
	// The stream closes with no content chunks → streamLLMToEventBus returns fullContent="".
	// streamThinkingToEventBus wraps it as ChatResponse{Content:"", FinishReason:"stop"}.
	// analyzeResponse() returns verdict{isDone:true, finalAnswer:""} → BUG: empty answer.
	//
	// Prepare 3 responses for initial attempt + 2 retries (after fix).
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{{Done: true}}},
			{chunks: []types.StreamResponse{{Done: true}}},
			{chunks: []types.StreamResponse{{Done: true}}},
		},
	}

	engine := newTestEngine(t, mock)
	state := &types.AgentState{}
	ctx := context.Background()

	_, err := engine.executeLoop(ctx, state, "test query", emptyMessages(), emptyTools(), "sess-1", "msg-1")

	assert.NoError(t, err)
	assert.True(t, state.IsComplete)
	assert.NotEmpty(t, state.FinalAnswer,
		"BUG: FinalAnswer is empty when LLM returns empty content with stop. "+
			"analyzeResponse() should not allow empty content to be accepted as final answer.")
}

// ---------------------------------------------------------------------------
// TC2: Non-empty content + stop → normal completion (regression guard)
// ---------------------------------------------------------------------------

func TestExecuteLoop_NonEmptyContentWithStop_ShouldComplete(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{Content: "Here is my answer", Done: true},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	state := &types.AgentState{}
	ctx := context.Background()

	_, err := engine.executeLoop(ctx, state, "test query", emptyMessages(), emptyTools(), "sess-1", "msg-1")

	assert.NoError(t, err)
	assert.True(t, state.IsComplete)
	assert.Equal(t, "Here is my answer", state.FinalAnswer)
}

// ---------------------------------------------------------------------------
// TC4: Empty → retry with nudge → non-empty → success
// ---------------------------------------------------------------------------

func TestExecuteLoop_EmptyThenNonEmpty_ShouldRetryAndComplete(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			// Round 1: empty content → triggers retry + nudge
			{chunks: []types.StreamResponse{{Done: true}}},
			// Round 2: after nudge, LLM produces answer
			{chunks: []types.StreamResponse{
				{Content: "Here is the answer.", Done: true},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	state := &types.AgentState{}
	ctx := context.Background()

	_, err := engine.executeLoop(ctx, state, "test query", emptyMessages(), emptyTools(), "sess-1", "msg-1")

	assert.NoError(t, err)
	assert.True(t, state.IsComplete)
	assert.Equal(t, "Here is the answer.", state.FinalAnswer)
}

// ---------------------------------------------------------------------------
// TC5: FinishReason propagation through streamThinkingToEventBus
// ---------------------------------------------------------------------------

func TestStreamThinkingToEventBus_PropagatesFinishReason(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantReason   string
	}{
		{"stop", "stop", "stop"},
		{"tool_calls", "tool_calls", "tool_calls"},
		{"length", "length", "length"},
		{"empty_fallback", "", "stop"}, // empty FinishReason → fallback to "stop"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockChat{
				responses: []mockResponse{
					{chunks: []types.StreamResponse{
						{Content: "test content", Done: true, FinishReason: tt.finishReason},
					}},
				},
			}

			engine := newTestEngine(t, mock)
			ctx := context.Background()
			msgs := []chat.Message{{Role: "user", Content: "test"}}
			tools := []chat.Tool{}

			resp, err := engine.streamThinkingToEventBus(ctx, msgs, tools, 0, "sess-1")

			assert.NoError(t, err)
			assert.Equal(t, tt.wantReason, resp.FinishReason)
		})
	}
}

func TestStreamThinkingToEventBus_SetsCompletionTokenBudget(t *testing.T) {
	t.Run("honors an explicit 4096 budget", func(t *testing.T) {
		mock := &mockChat{
			responses: []mockResponse{
				{chunks: []types.StreamResponse{{Content: "ok", Done: true, FinishReason: "stop"}}},
			},
		}
		engine := newTestEngine(t, mock, withMaxCompletionTokens(4096))
		_, err := engine.streamThinkingToEventBus(context.Background(),
			[]chat.Message{{Role: "user", Content: "test"}}, nil, 0, "sess-1")
		require.NoError(t, err)
		require.Len(t, mock.opts, 1)
		assert.Zero(t, mock.opts[0].MaxTokens)
		assert.Equal(t, 4096, mock.opts[0].MaxCompletionTokens)
	})

	t.Run("defaults when unset", func(t *testing.T) {
		mock := &mockChat{
			responses: []mockResponse{
				{chunks: []types.StreamResponse{{Content: "ok", Done: true, FinishReason: "stop"}}},
			},
		}
		engine := newTestEngine(t, mock)
		_, err := engine.streamThinkingToEventBus(context.Background(),
			[]chat.Message{{Role: "user", Content: "test"}}, nil, 0, "sess-1")
		require.NoError(t, err)
		require.Len(t, mock.opts, 1)
		assert.Zero(t, mock.opts[0].MaxTokens)
		assert.Equal(t, types.DefaultSmartReasoningMaxCompletionTokens, mock.opts[0].MaxCompletionTokens)
	})

	t.Run("defaults to the write-file budget when a sandbox is bound", func(t *testing.T) {
		mock := &mockChat{
			responses: []mockResponse{
				{chunks: []types.StreamResponse{{Content: "ok", Done: true, FinishReason: "stop"}}},
			},
		}
		engine := newTestEngine(t, mock, func(cfg *types.AgentConfig) {
			cfg.SandboxConfigID = "cfg-a"
		})
		_, err := engine.streamThinkingToEventBus(context.Background(),
			[]chat.Message{{Role: "user", Content: "test"}}, nil, 0, "sess-1")
		require.NoError(t, err)
		require.Len(t, mock.opts, 1)
		assert.Zero(t, mock.opts[0].MaxTokens)
		assert.Equal(t, types.DefaultAgentMaxCompletionTokens, mock.opts[0].MaxCompletionTokens)
	})

	t.Run("preserves explicit higher budget", func(t *testing.T) {
		mock := &mockChat{
			responses: []mockResponse{
				{chunks: []types.StreamResponse{{Content: "ok", Done: true, FinishReason: "stop"}}},
			},
		}
		engine := newTestEngine(t, mock, withMaxCompletionTokens(64000))
		_, err := engine.streamThinkingToEventBus(context.Background(),
			[]chat.Message{{Role: "user", Content: "test"}}, nil, 0, "sess-1")
		require.NoError(t, err)
		require.Len(t, mock.opts, 1)
		assert.Zero(t, mock.opts[0].MaxTokens)
		assert.Equal(t, 64000, mock.opts[0].MaxCompletionTokens)
	})
}

// TestStreamThinkingToEventBus_RoutesReasoningAndAnswerSeparately is the
// regression guard for the "answer first shows under Thinking, then jumps to
// the answer area" UX bug. A natural-stop response that carries reasoning in
// the dedicated reasoning channel (ResponseTypeThinking) plus plain answer
// content (ResponseTypeAnswer) must route the reasoning to thought events and
// the answer live to final-answer events — never the reverse.
func TestStreamThinkingToEventBus_RoutesReasoningAndAnswerSeparately(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeThinking, Content: "let me reason"},
				{ResponseType: types.ResponseTypeThinking, Content: "", Done: true},
				{ResponseType: types.ResponseTypeAnswer, Content: "The answer "},
				{ResponseType: types.ResponseTypeAnswer, Content: "is 42.", Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	var thoughts, answers string
	engine.eventBus.On(event.EventAgentThought, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentThoughtData); ok {
			thoughts += d.Content
		}
		return nil
	})
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentFinalAnswerData); ok {
			answers += d.Content
		}
		return nil
	})

	resp, err := engine.streamThinkingToEventBus(context.Background(),
		emptyMessages(), emptyTools(), 0, "sess-1")
	require.NoError(t, err)

	assert.Equal(t, "let me reason", thoughts, "reasoning_content must stream to thought events")
	assert.Equal(t, "The answer is 42.", answers, "plain answer content must stream live to final-answer events")
	assert.True(t, resp.AnswerStreamed, "AnswerStreamed must be set when answer text was streamed live")
	assert.NotEmpty(t, resp.AnswerEventID, "AnswerEventID must identify the live answer stream")
}

// TestStreamThinkingToEventBus_SplitsInlineThinkBlock verifies that models which
// embed reasoning inline as <think>…</think> in the content channel still have
// their reasoning routed to thought events and only the real answer streamed to
// the final-answer area.
func TestStreamThinkingToEventBus_SplitsInlineThinkBlock(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{
					ResponseType: types.ResponseTypeAnswer, Content: "<think>hidden reasoning</think>Visible answer.",
					Done: true, FinishReason: "stop",
				},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	var thoughts, answers string
	engine.eventBus.On(event.EventAgentThought, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentThoughtData); ok {
			thoughts += d.Content
		}
		return nil
	})
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentFinalAnswerData); ok {
			answers += d.Content
		}
		return nil
	})

	_, err := engine.streamThinkingToEventBus(context.Background(),
		emptyMessages(), emptyTools(), 0, "sess-1")
	require.NoError(t, err)

	assert.Equal(t, "hidden reasoning", thoughts, "inline <think> content must route to thought events")
	assert.Equal(t, "Visible answer.", answers, "answer outside <think> must stream to final-answer events")
}

// TestExecuteLoop_NaturalStop_DoesNotDuplicateAnswer ensures the natural-stop
// branch does not re-emit the full answer (it was already streamed live), so
// the final-answer content appears exactly once instead of streaming under
// Thinking and then "jumping" to a duplicate answer block.
func TestExecuteLoop_NaturalStop_DoesNotDuplicateAnswer(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "Hello "},
				{ResponseType: types.ResponseTypeAnswer, Content: "world", Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	var answerContent string
	var doneCount int
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		if d, ok := evt.Data.(event.AgentFinalAnswerData); ok {
			answerContent += d.Content
			if d.Done {
				doneCount++
			}
		}
		return nil
	})

	state := &types.AgentState{}
	_, err := engine.executeLoop(context.Background(), state, "test query",
		emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.True(t, state.IsComplete)
	assert.Equal(t, "Hello world", state.FinalAnswer)
	assert.Equal(t, "Hello world", answerContent,
		"answer content must be emitted exactly once (streamed live, not re-emitted by the natural-stop branch)")
	assert.GreaterOrEqual(t, doneCount, 1, "a Done marker must close the answer stream")
}

// TestExecuteLoop_EndTurnTerminates ensures Anthropic-style end_turn is treated
// like OpenAI's stop when no tool calls are present. Otherwise the ReAct loop
// keeps asking the model again and streams repeated answer chunks.
func TestExecuteLoop_EndTurnTerminates(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "The answer.", Done: true, FinishReason: "end_turn"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	state := &types.AgentState{}
	_, err := engine.executeLoop(context.Background(), state, "test query",
		emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.True(t, state.IsComplete)
	assert.Equal(t, "The answer.", state.FinalAnswer)
	assert.Equal(t, 1, mock.callCount, "end_turn must end the loop after the first model call")
}

func TestStreamFinalAnswerToEventBus_EmitsDoneWhenProviderEndsWithEmptyChunk(t *testing.T) {
	mock := &mockChat{
		responses: []mockResponse{
			{chunks: []types.StreamResponse{
				{ResponseType: types.ResponseTypeAnswer, Content: "final answer", Done: false},
				{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
			}},
		},
	}

	engine := newTestEngine(t, mock)
	var finalAnswerEvents []event.AgentFinalAnswerData
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		require.True(t, ok)
		finalAnswerEvents = append(finalAnswerEvents, data)
		return nil
	})

	state := &types.AgentState{}
	err := engine.streamFinalAnswerToEventBus(context.Background(), "test query", state, "sess-1", emptyMessages())

	require.NoError(t, err)
	require.Len(t, finalAnswerEvents, 2)
	assert.False(t, finalAnswerEvents[0].Done)
	assert.True(t, finalAnswerEvents[1].Done)
	assert.Equal(t, "final answer", finalAnswerEvents[0].Content+finalAnswerEvents[1].Content,
		"a decoder may hold a short suffix until Done to rule out a split model handle")
	assert.Equal(t, "final answer", state.FinalAnswer)
}

func TestIsTransientErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"rate limited", &api.HTTPError{StatusCode: 429}, true},
		{"server error", fmt.Errorf("chat: %w", &api.HTTPError{StatusCode: 503}), true},
		{
			"bad request mentioning a status",
			&api.HTTPError{StatusCode: 400, Body: "max_tokens 5000 > 500 timeout"}, false,
		},
		{"transport", &api.TransportError{Op: "send request", Err: errors.New("dial tcp: refused")}, true},
		{"unexpected eof", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		{"user stop", fmt.Errorf("round: %w", context.Canceled), false},
		{"stream text with status", errors.New("LLM stream error: upstream returned 502"), true},
		{"stream text with longer number", errors.New("LLM stream error: max_tokens must be <= 5000"), false},
		{"stream ended early", fmt.Errorf("LLM stream error: %s", types.StreamEndedEarlyError), true},
		{"invalid key", errors.New("LLM stream error: invalid api key"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isTransientError(tc.err))
		})
	}
}

func TestStreamLLMIncompleteFinishIsAStreamError(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "partial"},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: types.FinishReasonIncomplete},
	}}}}
	engine := newTestEngine(t, model)

	_, err := engine.streamLLMToEventBus(context.Background(), nil, nil, nil)

	require.Error(t, err, "a stream cut before its finish reason is not a completed turn")
	require.True(t, isTransientError(err))
}

// These requests use the real provider, SSE decoder, assembler and Agent retry
// control. A model mock cannot establish this contract.
func TestB2AgentRealHTTPAndSSERetryControl(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantCalls int32
		wantError bool
	}{
		{"HTTP429", 429, `{"error":{"message":"B2_PROVIDER_CANARY Bearer B2_TOKEN_CANARY"}}`, 2, false},
		{"HTTP503", 503, `{"error":{"message":"B2_SQL_CANARY"}}`, 2, false},
		{"SSEServer", 200, "data: {\"error\":{\"code\":\"server_error\",\"message\":\"B2_PROVIDER_CANARY\"}}\n\n", 2, false},
		{"MessageOnlyOverflow", 200, "data: {\"error\":{\"message\":\"context_length_exceeded rate limit 502 B2_TOKEN_CANARY\"}}\n\n", 1, true},
		{"Malformed502", 200, "data: <html>502 B2_PROVIDER_CANARY</html>\n\n", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			capture := &a5LogCapture{}
			logger.SetOutput(io.MultiWriter(os.Stderr, capture))
			t.Cleanup(func() { logger.SetOutput(os.Stderr) })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				n := requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2", Client: srv.Client()}, Settings: api.DefaultOpenAICompletions()})
			engine := newTestEngine(t, model)
			engine.toolRegistry = agenttools.NewToolRegistry()
			tool := newCountingTool("write_sandbox_file")
			engine.toolRegistry.RegisterTool(tool)
			messages := emptyMessages()
			response, err := engine.callLLMWithRetry(context.Background(), &messages, nil, &types.AgentState{}, "safe query", 0, "synthetic-session")
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.Equal(t, "complete", response.Content)
			}
			require.Equal(t, tc.wantCalls, requests.Load())
			require.False(t, engine.overflowRecovered)
			require.Zero(t, tool.calls)
			for _, needle := range []string{"B2_PROVIDER_CANARY", "B2_TOKEN_CANARY", "B2_SQL_CANARY", "Bearer "} {
				require.NotContains(t, capture.String(), needle)
				if err != nil {
					require.NotContains(t, err.Error(), needle)
				}
			}
			t.Logf("HTTP/SSE requests=%d retries=%d overflow=false tool_calls=%d", requests.Load(), requests.Load()-1, tool.calls)
		})
	}
}

func TestB2AgentTrustedOverflowCompactsOnlyOnce(t *testing.T) {
	var mu sync.Mutex
	purposes := map[string]int{}
	capture := &a5LogCapture{}
	logger.SetOutput(io.MultiWriter(os.Stderr, capture))
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid synthetic request", 400)
			return
		}
		purpose := "initial"
		for _, m := range body.Messages {
			if strings.Contains(m.Content, "The messages above are a conversation to summarize.") {
				purpose = "history"
				break
			}
			if strings.Contains(m.Content, "This is the PREFIX of a turn") {
				purpose = "prefix"
				break
			}
			if strings.Contains(m.Content, "The conversation history before this point was compacted into the following summary:") {
				purpose = "recovery"
			}
		}
		mu.Lock()
		purposes[purpose]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if purpose == "initial" {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"B2_OVERFLOW_CANARY"}}`)
			return
		}
		text := map[string]string{"history": "HISTORY_SUMMARY", "prefix": "PREFIX_SUMMARY", "recovery": "RECOVERED_ANSWER"}[purpose]
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
	}))
	defer srv.Close()
	model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2", Client: srv.Client()}, Settings: api.DefaultOpenAICompletions()})
	engine := newTestEngine(t, model, withMaxContextTokens(40000), withMaxCompletionTokens(4000))
	engine.toolRegistry = agenttools.NewToolRegistry()
	tool := newCountingTool("write_sandbox_file")
	engine.toolRegistry.RegisterTool(tool)
	var compactedEvents []event.ContextCompactedData
	engine.eventBus.On(event.EventContextCompacted, func(_ context.Context, evt event.Event) error {
		compactedEvents = append(compactedEvents, evt.Data.(event.ContextCompactedData))
		return nil
	})
	sink := &recordingCheckpointSink{}
	engine.SetContextCheckpointSink(sink)
	messages := storedHistoryOverflow()
	before := len(messages)
	response, err := engine.callLLMWithRetry(context.Background(), &messages, nil, &types.AgentState{}, "safe query", 0, "synthetic-session")
	require.NoError(t, err)
	require.Equal(t, "RECOVERED_ANSWER", response.Content)
	mu.Lock()
	actual := map[string]int{}
	total := 0
	for k, v := range purposes {
		actual[k] = v
		total += v
	}
	mu.Unlock()
	require.Equal(t, map[string]int{"initial": 1, "history": 1, "prefix": 1, "recovery": 1}, actual)
	require.Equal(t, 4, total)
	require.True(t, engine.overflowRecovered)
	require.EqualValues(t, 1, engine.contextRewrites)
	require.Len(t, compactedEvents, 1, "actual compaction lifecycle event, independent of sink count")
	require.Equal(t, "overflow", compactedEvents[0].Reason)
	require.True(t, compactedEvents[0].SplitTurn)
	require.False(t, compactedEvents[0].Degraded)
	require.Equal(t, 1, strings.Count(capture.String(), "Compacted (overflow):"))
	require.Equal(t, 1, strings.Count(capture.String(), "Provider rejected the request as too large; compacting and retrying once"))
	require.Less(t, len(messages), before)
	require.Len(t, sink.saved, 1)
	require.Equal(t, "HISTORY_SUMMARY", sink.saved[0].Summary)
	require.False(t, sink.saved[0].Degraded)
	require.Contains(t, compactedEvents[0].Summary, "PREFIX_SUMMARY")
	require.Contains(t, compactedEvents[0].Summary, "HISTORY_SUMMARY")
	require.Zero(t, tool.calls)
	require.NotContains(t, capture.String(), "B2_OVERFLOW_CANARY")
	t.Logf("purpose_counts=%v total=%d actual_compaction_events=%d checkpoints=%d split_turn=true degraded=false tools=%d", actual, total, len(compactedEvents), len(sink.saved), tool.calls)
}

func TestB2AgentRealGoogleSafetyDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"promptFeedback\":{\"blockReason\":\"SAFETY\"}}\n\n")
	}))
	defer srv.Close()
	model := googlegenai.New(googlegenai.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2-google", Client: srv.Client()}, Settings: api.DefaultGoogleGenerativeAI()})
	engine := newTestEngine(t, model)
	messages := emptyMessages()
	_, err := engine.callLLMWithRetry(context.Background(), &messages, nil, &types.AgentState{}, "safe query", 0, "synthetic-session")
	var failure *types.StreamFailure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, types.StreamFailureSafetyBlocked, failure.Code())
	require.EqualValues(t, 1, calls.Load())
	require.False(t, engine.overflowRecovered)
	t.Logf("google_safety requests=%d compaction=0 result=blocked", calls.Load())
}

func TestB2AgentRealContextAndProviderTimeout(t *testing.T) {
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	for _, mode := range []string{"parent_cancel", "parent_deadline", "provider_deadline"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			started := make(chan struct{})
			requestCanceled := make(chan struct{})
			firstHandlerExited := make(chan struct{})
			teardown := make(chan struct{})
			modelExited := make(chan struct{})
			var teardownExits atomic.Int32
			var activeHandlers atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				activeHandlers.Add(1)
				defer activeHandlers.Add(-1)
				_, bodyErr := io.Copy(io.Discard, r.Body)
				closeErr := r.Body.Close()
				if bodyErr != nil || closeErr != nil {
					t.Error("synthetic request body could not be consumed and closed")
					return
				}
				if calls.Add(1) == 1 {
					defer close(firstHandlerExited)
					close(started)
					select {
					case <-r.Context().Done():
						close(requestCanceled)
					case <-teardown:
						teardownExits.Add(1)
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer func() {
				// Teardown is an independent escape path, never cancellation evidence.
				close(teardown)
				srv.Close()
				modelDidExit := false
				select {
				case <-modelExited:
					modelDidExit = true
				case <-time.After(10 * time.Second):
					t.Error("model goroutine did not exit during teardown")
				}
				assert.Zero(t, activeHandlers.Load())
				t.Logf("mode=%s teardown_exits=%d active_handlers=%d model_exited=%t", mode, teardownExits.Load(), activeHandlers.Load(), modelDidExit)
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := srv.Client()
			if mode == "parent_deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				defer cancel()
			}
			if mode == "provider_deadline" {
				client.Timeout = time.Second
			}
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2", Client: client}, Settings: api.DefaultOpenAICompletions()})
			engine := newTestEngine(t, model)
			messages := emptyMessages()
			finished := make(chan error, 1)
			go func() {
				defer close(modelExited)
				_, err := engine.callLLMWithRetry(ctx, &messages, nil, &types.AgentState{}, "safe query", 0, "synthetic-session")
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("request did not reach provider")
			}
			if mode == "parent_cancel" {
				cancel()
			}
			var err error
			select {
			case err = <-finished:
			case <-time.After(10 * time.Second):
				cancel()
				t.Fatal("bounded control chain did not finish")
			}
			if mode == "provider_deadline" {
				require.NoError(t, err)
				require.NoError(t, ctx.Err())
				require.EqualValues(t, 2, calls.Load())
			} else {
				require.Error(t, err)
				require.EqualValues(t, 1, calls.Load())
				if mode == "parent_cancel" {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			}
			select {
			case <-requestCanceled:
			case <-time.After(10 * time.Second):
				t.Fatal("provider did not observe real request context cancellation")
			}
			select {
			case <-firstHandlerExited:
			case <-time.After(10 * time.Second):
				t.Fatal("canceled provider handler did not exit")
			}
			require.Zero(t, teardownExits.Load())
			require.False(t, engine.overflowRecovered)
			t.Logf("mode=%s requests=%d compaction=0 parent_error=%v request_context_canceled=true first_handler_exited=true teardown_exits=0", mode, calls.Load(), ctx.Err())
		})
	}
}

func TestB2AgentRealTemporaryBudgetAndExecutedTool(t *testing.T) {
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	for _, exhaust := range []bool{false, true} {
		t.Run(fmt.Sprintf("exhaust=%v", exhaust), func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if exhaust || n == 1 {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"message":"B2_PROVIDER_CANARY"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2", Client: srv.Client()}, Settings: api.DefaultOpenAICompletions()})
			engine := newTestEngine(t, model)
			engine.toolRegistry = agenttools.NewToolRegistry()
			tool := newCountingTool("test_history")
			engine.toolRegistry.RegisterTool(tool)
			messages := emptyMessages()
			if !exhaust {
				tc := types.LLMToolCall{ID: "already-executed", Function: types.FunctionCall{Name: "test_history", Arguments: "{}"}}
				result := engine.runToolCall(context.Background(), tc, 0, 0, 0, "synthetic-session", "synthetic-message")
				require.True(t, result.Result.Success)
				require.Equal(t, 1, tool.calls)
				messages = append(messages, chat.Message{Role: "assistant", ToolCalls: []api.ToolCall{{ID: tc.ID, Type: "function", Function: api.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments}}}}, chat.Message{Role: "tool", ToolCallID: tc.ID, Content: result.Result.Output})
			}
			response, err := engine.callLLMWithRetry(context.Background(), &messages, nil, &types.AgentState{}, "safe query", 1, "synthetic-session")
			if exhaust {
				require.Error(t, err)
				require.Nil(t, response)
				require.EqualValues(t, 1+maxLLMRetries, requests.Load())
				require.Zero(t, tool.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, "complete", response.Content)
				require.EqualValues(t, 2, requests.Load())
				require.Equal(t, 1, tool.calls)
			}
			require.False(t, engine.overflowRecovered)
			t.Logf("requests=%d retries=%d tool_execution=%d compaction=0 exhaust=%v", requests.Load(), requests.Load()-1, tool.calls, exhaust)
		})
	}
}

type b2RetryNotice struct {
	capture a5LogCapture
	once    sync.Once
	notice  chan struct{}
}

func (w *b2RetryNotice) Write(p []byte) (int, error) {
	n, err := w.capture.Write(p)
	if strings.Contains(string(p), "retrying in 30s") {
		w.once.Do(func() { close(w.notice) })
	}
	return n, err
}

func TestB2AgentRealRetryAfterWaitAndCap(t *testing.T) {
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	for _, capped := range []bool{false, true} {
		t.Run(fmt.Sprintf("capped=%v", capped), func(t *testing.T) {
			var calls atomic.Int32
			var mu sync.Mutex
			var first, second time.Time
			notice := &b2RetryNotice{notice: make(chan struct{})}
			logger.SetOutput(io.MultiWriter(os.Stderr, notice))
			t.Cleanup(func() { logger.SetOutput(os.Stderr) })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				mu.Lock()
				if n == 1 {
					first = time.Now()
				} else {
					second = time.Now()
				}
				mu.Unlock()
				if n == 1 {
					if capped {
						w.Header().Set("Retry-After", "3600")
					} else {
						w.Header().Set("Retry-After", "2")
					}
					w.WriteHeader(429)
					io.WriteString(w, `{"error":{"message":"B2_TOKEN_CANARY"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "b2", Client: srv.Client()}, Settings: api.DefaultOpenAICompletions()})
			engine := newTestEngine(t, model)
			messages := emptyMessages()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := engine.callLLMWithRetry(ctx, &messages, nil, &types.AgentState{}, "safe query", 0, "synthetic-session")
				finished <- err
			}()
			if capped {
				select {
				case <-notice.notice:
					cancel()
				case <-time.After(10 * time.Second):
					t.Fatal("bounded 30s retry policy not observed")
				}
			}
			var err error
			select {
			case err = <-finished:
			case <-time.After(10 * time.Second):
				cancel()
				t.Fatal("retry control timed out")
			}
			if capped {
				var failure *types.StreamFailure
				require.ErrorAs(t, err, &failure)
				require.Equal(t, types.StreamFailureRateLimited, failure.Code())
				require.Equal(t, 30*time.Second, failure.RetryAfter())
				require.EqualValues(t, 1, calls.Load())
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 2, calls.Load())
				mu.Lock()
				elapsed := second.Sub(first)
				mu.Unlock()
				require.GreaterOrEqual(t, elapsed, 2*time.Second)
			}
			require.NotContains(t, notice.capture.String(), "B2_TOKEN_CANARY")
			require.False(t, engine.overflowRecovered)
			t.Logf("capped=%v requests=%d compaction=0; cap waiting cancelled by parent after policy signal", capped, calls.Load())
		})
	}
}
