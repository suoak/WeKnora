package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRetrievalLoopSynthesizesWithoutExecutingMoreTools(t *testing.T) {
	call := types.LLMToolCall{ID: "retrieval", Type: "function", Function: types.FunctionCall{Name: "read_document", Arguments: `{}`}}
	for _, ignoredBudget := range []bool{false, true} {
		model := &mockChat{}
		for i := 0; i < 3; i++ {
			model.responses = append(model.responses, mockResponse{chunks: []types.StreamResponse{{Done: true, FinishReason: "tool_calls", ToolCalls: []types.LLMToolCall{call}}}})
		}
		last := types.StreamResponse{Done: true, FinishReason: "stop", Content: "Complete answer from verified sources."}
		if ignoredBudget {
			last = types.StreamResponse{Done: true, FinishReason: "tool_calls", ToolCalls: []types.LLMToolCall{call}}
		}
		model.responses = append(model.responses, mockResponse{chunks: []types.StreamResponse{last}})
		engine := newTestEngine(t, model)
		tool := newCountingTool("read_document")
		engine.toolRegistry.RegisterTool(tool)
		state, err := engine.executeLoop(context.Background(), &types.AgentState{}, "question", emptyMessages(), []chat.Tool{}, "session", "message")
		require.NoError(t, err)
		require.True(t, state.IsComplete)
		require.Equal(t, 3, tool.calls, "a model ignoring the synthesis budget must not execute a fourth tool")
		require.Empty(t, model.opts[3].Tools)
		require.NotEmpty(t, state.FinalAnswer)
	}
}

func retrievalCall(name, id string) types.ToolCall {
	return types.ToolCall{Name: name, Result: &types.ToolResult{Success: true, Data: map[string]any{"chunks": []map[string]any{{"chunk_id": id}}}}}
}

func TestRetrievalProgressCountsEvidenceNotChangingQueries(t *testing.T) {
	var progress retrievalProgress
	now := time.Now()
	for i := 0; i < 4; i++ {
		progress.observe([]types.ToolCall{retrievalCall("search_knowledge", "same-chunk")}, now)
	}
	require.True(t, progress.needsSynthesis())
	require.False(t, progress.exhausted(now))
	progress.observe([]types.ToolCall{retrievalCall("read_document", "new-chunk")}, now)
	require.Zero(t, progress.unchanged)
	require.True(t, progress.exhausted(now.Add(retrievalTurnBudget)))
}

func TestRetrievalFailuresAndOtherWorkflowBudgets(t *testing.T) {
	var progress retrievalProgress
	now := time.Now()
	for i := 0; i < 3; i++ {
		progress.observe([]types.ToolCall{{Name: "read_document", Result: &types.ToolResult{Success: false, Error: "source denied"}}}, now)
	}
	require.True(t, progress.needsSynthesis())
	require.Empty(t, progress.seen)
	progress.synthesis = true
	require.False(t, progress.needsSynthesis())
	progress.observe([]types.ToolCall{retrievalCall("execute_shell", "write-result")}, now)
	require.False(t, progress.exhausted(now.Add(24*time.Hour)), "coding and other workflows retain existing budgets")
}
