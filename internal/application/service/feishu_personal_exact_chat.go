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
	if !exactPersonalRecoveryEnabled() || p == nil || request == nil {
		return ctx, options, nil
	}
	inputs := request.SearchResult
	if len(request.RerankResult) != 0 {
		inputs = request.RerankResult
	}
	if len(inputs) == 0 || inputs[0] == nil {
		return ctx, options, nil
	}
	knowledgeID := inputs[0].KnowledgeID
	var count int64
	if err := p.service.db.WithContext(ctx).Model(&types.TaskPendingOp{}).Where(
		"scope=? AND scope_id=?", types.PersonalExactReceiptScope, knowledgeID,
	).Count(&count).Error; err != nil {
		return ctx, options, err
	}
	if count == 0 {
		return ctx, options, nil // Ordinary model/source policy remains authoritative.
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
	read, err := p.exactPublishedReadContext(ctx, knowledgeID)
	if err != nil {
		return ctx, options, err
	}
	_, tenant, _, kbID, _ := types.ExactPublishedDocumentIdentity(read)
	owner, err := ks.repo.GetKnowledgeByID(read, tenant, knowledgeID)
	if err != nil {
		return ctx, options, err
	}
	ids := make([]string, 0, len(inputs))
	for _, result := range inputs {
		if result == nil || result.KnowledgeID != knowledgeID || result.KnowledgeBaseID != kbID {
			return ctx, options, errPersonalExactDocument
		}
		ids = append(ids, result.ID)
	}
	rows, err := ks.chunkRepo.ListChunksByID(read, tenant, ids)
	if err != nil {
		return ctx, options, err
	}
	byID := make(map[string]*types.Chunk, len(rows))
	for _, row := range rows {
		if row.KnowledgeID != knowledgeID || row.KnowledgeBaseID != kbID || !row.IsEnabled {
			return ctx, options, errPersonalExactDocument
		}
		byID[row.ID] = row
	}
	canonical := make([]*types.SearchResult, 0, len(inputs))
	resultBuilder := &knowledgeBaseService{}
	for _, input := range inputs {
		row := byID[input.ID]
		if row == nil {
			return ctx, options, errPersonalExactDocument
		}
		canonical = append(canonical, resultBuilder.buildSearchResult(row, owner, input.Score, input.MatchType, ""))
	}
	trusted := request.Clone()
	trusted.SearchResult, trusted.RerankResult = canonical, nil
	if len(request.RerankResult) != 0 {
		trusted.SearchResult, trusted.RerankResult = nil, canonical
	}
	// No unproved model-generated completeness/axis annotation enters a prompt.
	trusted.RetrievalCompleteness = types.RetrievalCompleteness{}
	expected, err := chatpipeline.RenderVerifiedDocumentMessages(read, trusted, ks.chunkRepo)
	if err != nil {
		return ctx, options, err
	}
	var actualOptions *chat.ChatOptions
	if options != nil {
		copy := *options
		copy.PromptCacheKey = "" // Fresh proof cannot authorize historical cache input.
		actualOptions = &copy
	}
	expectedDigest, err := exactMessagesDigest(expected, actualOptions)
	if err != nil {
		return ctx, options, err
	}
	actualDigest, err := exactMessagesDigest(messages, actualOptions)
	if err != nil || actualDigest != expectedDigest {
		return ctx, options, errPersonalExactDocument
	}
	live := func(current context.Context) error {
		present, err := types.CheckExactPublishedDocumentRead(current)
		if err != nil {
			return err
		}
		if !present {
			return errPersonalExactDocument
		}
		return nil
	}
	work, err := withExactPersonalMessages(read, tenant, messages, actualOptions, live)
	return work, actualOptions, err
}

// Read authority is separate from model-input authority. It permits the normal
// merger to load only the independently proved document, including its parents.
func (s *modelService) AuthorizeExactPersonalRead(
	ctx context.Context, results []*types.SearchResult,
) (context.Context, error) {
	p := s.personalRecovery
	if !exactPersonalRecoveryEnabled() || p == nil || len(results) == 0 || results[0] == nil {
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
	read, err := s.AuthorizeExactPersonalRead(ctx, results)
	if err != nil {
		return ctx, nil, err
	}
	if _, _, _, _, exact := types.ExactPublishedDocumentIdentity(read); !exact {
		return ctx, model, nil
	}
	ks := s.personalRecovery.service.knowledge.(*knowledgeService)
	builder := &knowledgeBaseService{kgRepo: ks.repo, chunkRepo: ks.chunkRepo}
	guarded, err := builder.exactRerankModel(read, model, query, results)
	return read, guarded, err
}
