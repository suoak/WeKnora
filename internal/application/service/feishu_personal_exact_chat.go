package service

import (
	"context"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
)

// AuthorizeExactPersonalChat accepts only a fresh, text-only ordinary RAG turn.
// It rebuilds the actual model messages from canonical published outputs and
// server templates; a read context, result ID or caller-supplied digest alone
// can never authorize a model request. Unknown history/memory/media stay closed.
func (s *modelService) AuthorizeExactPersonalChat(
	ctx context.Context, request *types.ChatManage, messages []chat.Message, options *chat.ChatOptions,
) (context.Context, *chat.ChatOptions, error) {
	p := s.personalRecovery
	if p == nil || request == nil {
		return ctx, options, nil
	}
	inputs := request.SearchResult
	if len(request.RerankResult) != 0 {
		inputs = request.RerankResult
	}
	if len(inputs) == 0 || inputs[0] == nil {
		return ctx, options, nil
	}
	bundle, err := p.preparePublishedReadBundle(ctx, inputs)
	if err != nil {
		return ctx, options, err
	}
	if bundle == nil {
		return ctx, options, nil
	}
	if len(request.History) != 0 || request.MemoryPrompt != "" || request.QuotedContext != "" ||
		request.ImageDescription != "" || len(request.Images) != 0 || len(request.Attachments) != 0 ||
		request.SystemPromptOverride != "" || request.WebSearchEnabled || request.DataAnalysisEnabled ||
		request.GraphResult != nil || len(request.Entity) != 0 || len(request.EntityKnowledge) != 0 ||
		(request.Intent != "" && request.Intent != types.IntentKBSearch) ||
		(request.RewriteQuery != "" && request.RewriteQuery != request.Query) {
		return ctx, options, errPersonalExactDocument
	}
	ks := p.service.knowledge.(*knowledgeService)
	if ks.config == nil || ks.config.Conversation == nil || ks.config.Conversation.Summary == nil {
		return ctx, options, errPersonalExactDocument
	}
	defaults := ks.config.Conversation.Summary
	if request.SummaryConfig.Prompt != defaults.Prompt ||
		request.SummaryConfig.ContextTemplate != defaults.ContextTemplate {
		return ctx, options, errPersonalExactDocument
	}
	read := ctx
	tenant := types.MustTenantIDFromContext(ctx)
	canonical, err := bundle.canonicalResults(ctx, inputs)
	if err != nil {
		return ctx, options, err
	}
	trusted := request.Clone()
	trusted.SearchResult, trusted.RerankResult = canonical, nil
	if len(request.RerankResult) != 0 {
		trusted.SearchResult, trusted.RerankResult = nil, canonical
	}
	// No unproved model-generated completeness/axis annotation enters a prompt.
	trusted.RetrievalCompleteness = types.RetrievalCompleteness{}
	expected, err := chatpipeline.RenderVerifiedDocumentMessages(read, trusted, &publishedBundleChunks{ChunkRepository: ks.chunkRepo, bundle: bundle})
	if err != nil {
		return ctx, options, err
	}
	var actualOptions *chat.ChatOptions
	if options != nil {
		verifiedOptions := *options
		verifiedOptions.PromptCacheKey = "" // Fresh proof cannot authorize historical cache input.
		actualOptions = &verifiedOptions
	}
	expectedDigest, err := exactMessagesDigest(expected, actualOptions)
	if err != nil {
		return ctx, options, err
	}
	actualDigest, err := exactMessagesDigest(messages, actualOptions)
	if err != nil || actualDigest != expectedDigest {
		return ctx, options, errPersonalExactDocument
	}
	live := func(current context.Context) error { return bundle.validate(current) }
	work, err := withExactPersonalMessages(read, tenant, messages, actualOptions, live)
	return work, actualOptions, err
}

// Read authority is separate from model-input authority. It permits the normal
// merger to load only the independently proved document, including its parents.
func (s *modelService) AuthorizeExactPersonalRead(
	ctx context.Context, results []*types.SearchResult,
) (context.Context, error) {
	p := s.personalRecovery
	if p == nil || len(results) == 0 || results[0] == nil {
		return ctx, nil
	}
	knowledgeID := results[0].KnowledgeID
	var count int64
	if err := p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).Where(
		"scope=? AND scope_id=?", types.PersonalExactReceiptScope, knowledgeID,
	).Count(&count).Error; err != nil {
		return ctx, err
	}
	if count == 0 {
		return ctx, nil
	}
	read, err := p.exactPublishedReadContext(ctx, knowledgeID)
	if err != nil {
		return ctx, err
	}
	_, _, _, kb, _ := types.ExactPublishedDocumentIdentity(read)
	for _, result := range results {
		if result == nil || result.KnowledgeID != knowledgeID || result.KnowledgeBaseID != kb {
			return ctx, errPersonalExactDocument
		}
	}
	return read, nil
}

func (s *modelService) AuthorizeExactPersonalRerank(
	ctx context.Context, model rerank.Reranker, query string, results []*types.SearchResult,
) (context.Context, rerank.Reranker, error) {
	guarded, err := s.publishedBundleReranker(ctx, model, query, results)
	return ctx, guarded, err
}
