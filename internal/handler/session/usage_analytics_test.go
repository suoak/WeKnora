package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type usageMessageServiceStub struct {
	interfaces.MessageService
	updated bool
}

func (s *usageMessageServiceStub) UpdateMessage(_ context.Context, _ *types.Message) error {
	s.updated = true
	return nil
}
func (*usageMessageServiceStub) IndexMessageToKB(context.Context, string, string, string, string) {}

type usageRecorderStub struct {
	interfaces.UsageAnalyticsService
	messageService *usageMessageServiceStub
	tenantID       uint64
	message        *types.Message
}

func (s *usageRecorderStub) RecordAssistantTurn(_ context.Context, tenantID uint64, message *types.Message) error {
	if !s.messageService.updated {
		panic("analytics recorded before message persistence")
	}
	s.tenantID, s.message = tenantID, message
	return nil
}

func TestCompleteAssistantMessagePersistsUsageThenRecordsFrozenSessionTenant(t *testing.T) {
	messages := &usageMessageServiceStub{}
	recorder := &usageRecorderStub{messageService: messages}
	h := &Handler{messageService: messages, usageAnalytics: recorder}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(999))
	ctx = context.WithValue(ctx, types.SessionTenantIDContextKey, uint64(42))
	message := &types.Message{ID: "assistant-1", SessionID: "session-1", Usage: &types.TokenUsage{TotalTokens: 7}}
	h.completeAssistantMessage(ctx, message, "", "")
	require.True(t, messages.updated)
	require.Same(t, message, recorder.message)
	require.Equal(t, uint64(42), recorder.tenantID, "resource execution tenant must not replace the caller/session tenant")
	require.True(t, message.IsCompleted)
}
