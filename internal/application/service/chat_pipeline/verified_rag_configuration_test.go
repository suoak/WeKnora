package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestVerifiedRAGConfigurationRequiresPipelineAndExactRequest(t *testing.T) {
	request := &types.ChatManage{PipelineRequest: types.PipelineRequest{SessionID: "turn", UserID: "reader", Query: "original", Language: "English", SummaryConfig: types.SummaryConfig{Prompt: "server template", ContextTemplate: "{{contexts}} {{query}}"}}}
	ctx := WithVerifiedRAGConfiguration(context.Background(), request)
	require.False(t, HasVerifiedRAGConfiguration(ctx, request), "configuration alone does not attest a rewrite")
	request.RewriteQuery = "server model rewrite"
	recordVerifiedQueryRewrite(ctx, request)
	require.True(t, HasVerifiedRAGConfiguration(ctx, request))
	for _, mutate := range []func(*types.ChatManage){
		func(c *types.ChatManage) { c.Query = "other query" },
		func(c *types.ChatManage) { c.RewriteQuery = "unsealed rewrite" },
		func(c *types.ChatManage) { c.SessionID = "other turn" },
		func(c *types.ChatManage) { c.UserID = "other reader" },
		func(c *types.ChatManage) { c.SummaryConfig.Prompt = "client injected template" },
		func(c *types.ChatManage) { c.SummaryConfig.ContextTemplate = "unverified context template" },
	} {
		changed := request.Clone()
		mutate(changed)
		require.False(t, HasVerifiedRAGConfiguration(ctx, changed))
		recordVerifiedQueryRewrite(ctx, changed)
		require.False(t, HasVerifiedRAGConfiguration(ctx, changed), "an already sealed proof cannot be resealed")
	}
	require.False(t, HasVerifiedRAGConfiguration(context.Background(), request))
}
