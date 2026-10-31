package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type (
	exactPersonalMessagesKey struct{}
	exactPersonalPassagesKey struct{}
)

// Issued only by the trusted prompt builder AFTER validating every document
// dependency. A worker's document-read capability alone never opens a model.
type exactPersonalMessages struct {
	tenant uint64
	digest [32]byte
	live   func(context.Context) error
}

func exactMessagesDigest(messages []chat.Message, options *chat.ChatOptions) ([32]byte, error) {
	for _, message := range messages {
		if (message.Role != "system" && message.Role != "user") || message.Name != "" ||
			message.Kind != "" || message.TurnID != "" || message.ReasoningSignature != "" ||
			len(message.MultiContent) != 0 || len(message.Images) != 0 || len(message.ToolCalls) != 0 ||
			message.ToolCallID != "" || message.ReasoningContent != "" || len(message.ReasoningMetadata) != 0 {
			return [32]byte{}, errPersonalExactDocument
		}
	}
	if options != nil && (len(options.Tools) != 0 || len(options.Format) != 0 || options.PromptCacheKey != "") {
		return [32]byte{}, errPersonalExactDocument
	}
	data, err := json.Marshal(struct {
		Messages []chat.Message
		Options  *chat.ChatOptions
	}{messages, options})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func withExactPersonalMessages(
	ctx context.Context,
	tenant uint64,
	messages []chat.Message,
	options *chat.ChatOptions,
	live func(context.Context) error,
) (context.Context, error) {
	if tenant == 0 || live == nil {
		return ctx, errPersonalExactDocument
	}
	digest, err := exactMessagesDigest(messages, options)
	if err != nil {
		return ctx, err
	}
	if err = live(ctx); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, exactPersonalMessagesKey{}, exactPersonalMessages{tenant, digest, live}), nil
}

func checkExactPersonalMessages(
	ctx context.Context,
	tenant uint64,
	messages []chat.Message,
	options *chat.ChatOptions,
) (bool, error) {
	proof, ok := ctx.Value(exactPersonalMessagesKey{}).(exactPersonalMessages)
	if !ok {
		return false, nil
	}
	if tenant == 0 || proof.tenant != tenant || proof.live == nil {
		return true, errPersonalExactDocument
	}
	digest, err := exactMessagesDigest(messages, options)
	if err != nil || digest != proof.digest {
		return true, errPersonalExactDocument
	}
	return true, proof.live(ctx)
}

// The trusted retrieval builder must verify every passage before issuance.
// This capability is domain-separated from summary/chat and cannot be
// manufactured from a task payload or a document-read context.
func withExactPersonalPassages(
	ctx context.Context, tenant uint64, query string, passages []string, live func(context.Context) error,
) (context.Context, error) {
	if tenant == 0 || len(passages) == 0 || live == nil {
		return ctx, errPersonalExactDocument
	}
	data, err := json.Marshal(struct {
		Query    string
		Passages []string
	}{query, passages})
	if err != nil {
		return ctx, err
	}
	if err = live(ctx); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, exactPersonalPassagesKey{}, exactPersonalMessages{
		tenant: tenant, digest: sha256.Sum256(data), live: live,
	}), nil
}

func checkExactPersonalPassages(ctx context.Context, tenant uint64, query string, passages []string) (bool, error) {
	proof, ok := ctx.Value(exactPersonalPassagesKey{}).(exactPersonalMessages)
	if !ok {
		return false, nil
	}
	data, err := json.Marshal(struct {
		Query    string
		Passages []string
	}{query, passages})
	if err != nil || proof.tenant != tenant || tenant == 0 || len(passages) == 0 ||
		proof.live == nil || sha256.Sum256(data) != proof.digest {
		return true, errPersonalExactDocument
	}
	return true, proof.live(ctx)
}
