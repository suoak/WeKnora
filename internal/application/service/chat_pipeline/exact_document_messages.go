package chatpipeline

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// RenderVerifiedDocumentMessages reuses the ordinary merge, context and model
// handle renderers. The caller supplies independently verified canonical rows;
// this function issues no permission. It never persists a rendered message.
func RenderVerifiedDocumentMessages(
	ctx context.Context, request *types.ChatManage, chunks interfaces.ChunkRepository,
) ([]chat.Message, error) {
	work := request.Clone()
	// ChatManage.Clone deliberately omits retrieval outputs. Preserve only the
	// caller's already verified canonical rows, without sharing mutable results.
	cloneRows := func(rows []*types.SearchResult) []*types.SearchResult {
		out := make([]*types.SearchResult, 0, len(rows))
		for _, row := range rows {
			if row == nil {
				continue
			}
			copy := *row
			copy.SubChunkID = append([]string(nil), row.SubChunkID...)
			out = append(out, &copy)
		}
		return out
	}
	work.SearchResult, work.RerankResult = cloneRows(request.SearchResult), cloneRows(request.RerankResult)
	work.UserMessageID, work.MessageID = "", ""
	work.MergeResult, work.RenderedContexts, work.UserContent = nil, "", ""
	next := func() *PluginError { return nil }
	merge := &PluginMerge{chunkRepo: chunks}
	if merge.OnEvent(ctx, types.CHUNK_MERGE, work, next) != nil {
		return nil, errors.New("exact_document_merge_unverified")
	}
	into := &PluginIntoChatMessage{}
	if into.OnEvent(ctx, types.INTO_CHAT_MESSAGE, work, next) != nil {
		return nil, errors.New("exact_document_context_unverified")
	}
	messages, registry := prepareMessagesWithModelContext(ctx, work)
	return registry.EncodeMessages(messages), nil
}

type exactPersonalChatAuthorizer interface {
	AuthorizeExactPersonalChat(context.Context, *types.ChatManage, []chat.Message, *chat.ChatOptions) (
		context.Context, *chat.ChatOptions, error)
}

func prepareExactPersonalChat(
	ctx context.Context, models interfaces.ModelService, request *types.ChatManage,
	messages []chat.Message, options *chat.ChatOptions,
) (context.Context, *chat.ChatOptions, error) {
	if issuer, ok := models.(exactPersonalChatAuthorizer); ok {
		return issuer.AuthorizeExactPersonalChat(ctx, request, messages, options)
	}
	return ctx, options, nil
}

type exactPersonalReadAuthorizer interface {
	AuthorizeExactPersonalRead(context.Context, []*types.SearchResult) (context.Context, error)
}

func prepareExactPersonalRead(
	ctx context.Context, models interfaces.ModelService, results []*types.SearchResult,
) (context.Context, error) {
	if issuer, ok := models.(exactPersonalReadAuthorizer); ok {
		return issuer.AuthorizeExactPersonalRead(ctx, results)
	}
	return ctx, nil
}

type exactPersonalRerankAuthorizer interface {
	AuthorizeExactPersonalRerank(context.Context, rerank.Reranker, string, []*types.SearchResult) (
		context.Context, rerank.Reranker, error)
}

func prepareExactPersonalRerank(
	ctx context.Context, models interfaces.ModelService, model rerank.Reranker,
	query string, results []*types.SearchResult,
) (context.Context, rerank.Reranker, error) {
	if issuer, ok := models.(exactPersonalRerankAuthorizer); ok {
		return issuer.AuthorizeExactPersonalRerank(ctx, model, query, results)
	}
	return ctx, model, nil
}
