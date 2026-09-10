package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type usageChatStub struct {
	response *types.ChatResponse
	err      error
}

func (s *usageChatStub) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return s.response, s.err
}
func (*usageChatStub) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}
func (*usageChatStub) GetModelName() string { return "test" }
func (*usageChatStub) GetModelID() string   { return "model-1" }

func TestUsageRecordingChatIsOptInAndBestEffort(t *testing.T) {
	repo := &usageRepoStub{modelErr: errors.New("database unavailable")}
	recorder := NewUsageAnalyticsService(repo)
	inner := &usageChatStub{response: &types.ChatResponse{Content: "ok", Usage: types.TokenUsage{TotalTokens: 3}}}
	wrapped := &usageRecordingChat{inner: inner, recorder: recorder, modelID: "model-1", modelType: "knowledge_qa"}

	response, err := wrapped.Chat(context.Background(), nil, nil)
	if err != nil || response.Content != "ok" || repo.modelHit != 0 {
		t.Fatalf("unmarked call changed: response=%#v err=%v hits=%d", response, err, repo.modelHit)
	}

	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7})
	ctx = types.WithBackgroundModelUsage(ctx, types.ModelUsageOperationQueryRewrite, []string{"request-1"}, nil, nil)
	response, err = wrapped.Chat(ctx, nil, nil)
	if err != nil || response.Content != "ok" {
		t.Fatalf("analytics failure changed model result: response=%#v err=%v", response, err)
	}
	if repo.modelHit != 1 {
		t.Fatalf("record attempts = %d, want 1", repo.modelHit)
	}
}

func TestUsageRecordingChatDoesNotRecordProviderFailureOrZeroUsage(t *testing.T) {
	repo := &usageRepoStub{}
	recorder := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7})
	ctx = types.WithBackgroundModelUsage(ctx, types.ModelUsageOperationDocumentSummary, []string{"doc-1"}, nil, nil)

	failed := &usageRecordingChat{inner: &usageChatStub{err: errors.New("provider failed")}, recorder: recorder}
	if _, err := failed.Chat(ctx, nil, nil); err == nil {
		t.Fatal("provider error must be preserved")
	}
	empty := &usageRecordingChat{inner: &usageChatStub{response: &types.ChatResponse{Content: "ok"}}, recorder: recorder}
	if _, err := empty.Chat(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if repo.modelHit != 0 {
		t.Fatal("failed or unreported calls must not create events")
	}
}
