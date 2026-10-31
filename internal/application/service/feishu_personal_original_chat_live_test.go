//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"testing"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func exerciseOriginalExactChat(
	t *testing.T, f *personalRecoveryFixture, web context.Context, search *knowledgeBaseService,
) {
	t.Helper()
	f.ks.config.Conversation.Summary = &config.SummaryConfig{
		Prompt:          "Answer from the verified document context only. {{contexts}}",
		ContextTemplate: "{{contexts}}\n\nUser question: {{query}}",
	}
	thinking := false
	request := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			SessionID: "isolated-fresh-document-proof", UserID: "A", Query: "original",
			Language: "English", ChatModelID: "isolated-summary", RerankTopK: 10, EmbeddingTopK: 10,
			TenantID: types.MustTenantIDFromContext(web),
			SearchTargets: types.SearchTargets{
				&types.SearchTarget{
					Type:            types.SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: "d14eb0c0-1ade-46f9-890e-84fa823b335d",
					TenantID:        types.MustTenantIDFromContext(web),
				},
			},
			SummaryConfig: types.SummaryConfig{
				Prompt:          f.ks.config.Conversation.Summary.Prompt,
				ContextTemplate: f.ks.config.Conversation.Summary.ContextTemplate, MaxTokens: 256, Thinking: &thinking,
			},
		},
		PipelineState: types.PipelineState{RewriteQuery: "original"},
	}
	manager := chatpipeline.NewEventManager()
	lookup := chatpipeline.NewPluginSearch(manager, search, f.ks, nil, f.ks.config, nil, nil, nil, nil, nil)
	merge := chatpipeline.NewPluginMerge(manager, f.ks.chunkRepo, nil, f.ks.modelService)
	into := chatpipeline.NewPluginIntoChatMessage(manager, nil)
	completion := chatpipeline.NewPluginChatCompletion(manager, f.ks.modelService)
	next := func() *chatpipeline.PluginError { return nil }
	if err := lookup.OnEvent(web, types.CHUNK_SEARCH, request, next); err != nil {
		t.Fatalf("normal exact search rejected: %s", err.ErrorType)
	}
	require.NotEmpty(t, request.SearchResult)
	if err := merge.OnEvent(web, types.CHUNK_MERGE, request, next); err != nil {
		t.Fatalf("normal exact merge rejected: %s", err.ErrorType)
	}
	if err := into.OnEvent(web, types.INTO_CHAT_MESSAGE, request, next); err != nil {
		t.Fatalf("normal exact context rejected: %s", err.ErrorType)
	}
	if err := completion.OnEvent(web, types.CHAT_COMPLETION, request, next); err != nil {
		// Fixed category only: model errors can contain a provider response.
		t.Fatalf("normal exact chat rejected: %s", err.ErrorType)
	}
	require.NotNil(t, request.ChatResponse)
	require.NotEmpty(t, request.ChatResponse.Content)
	request.ChatResponse = nil
	request.History = []*types.History{{Query: "unknown prior question", Answer: "unknown historical answer"}}
	require.NotNil(t, completion.OnEvent(web, types.CHAT_COMPLETION, request, next))
	require.Nil(t, request.ChatResponse)
	request.History = nil
	request.UserContent += "\nUnproven cached content."
	require.NotNil(t, completion.OnEvent(web, types.CHAT_COMPLETION, request, next))
	require.Nil(t, request.ChatResponse)
}
