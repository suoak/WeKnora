package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// usageRecordingChat is deliberately opt-in: only calls carrying explicit
// ModelUsageMetadata are recorded. Streaming user-visible turns retain the
// Phase 1 message-ledger path and connectivity checks remain excluded.
type usageRecordingChat struct {
	inner     chat.Chat
	recorder  interfaces.UsageAnalyticsService
	modelID   string
	modelType string
}

func (c *usageRecordingChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	response, err := c.inner.Chat(ctx, messages, opts)
	if err != nil || response == nil || c.recorder == nil {
		return response, err
	}
	metadata, ok := types.ModelUsageMetadataFromContext(ctx)
	if !ok {
		return response, nil
	}
	metadata.ModelID = c.modelID
	metadata.ModelType = c.modelType
	metadata.Usage = response.Usage
	if recordErr := c.recorder.RecordModelInvocation(ctx, metadata); recordErr != nil {
		logger.ErrorWithFields(ctx, recordErr, map[string]interface{}{
			"event_key": metadata.EventKey,
			"operation": metadata.Operation,
		})
	}
	return response, nil
}

func (c *usageRecordingChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return c.inner.ChatStream(ctx, messages, opts)
}

func (c *usageRecordingChat) GetModelName() string { return c.inner.GetModelName() }
func (c *usageRecordingChat) GetModelID() string   { return c.inner.GetModelID() }
