package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type forgedPersonalModelContextKey string

func TestExactPersonalModelChecksActualMessagesAndLiveAuthority(t *testing.T) {
	gate := &legacySpyGate{}
	gate.denied.Store(true)
	spy := &legacySpyModel{}
	model := &legacyGuardedChat{inner: spy, gate: gate, tenant: 1}
	messages := []chat.Message{{Role: "system", Content: "summarize"}, {Role: "user", Content: "verified document"}}
	options := &chat.ChatOptions{MaxTokens: 100}
	valid := true
	ctx, err := withExactPersonalMessages(context.Background(), 1, messages, options, func(context.Context) error {
		if !valid {
			return errPersonalExactDocument
		}
		return nil
	})
	require.NoError(t, err)
	_, err = model.Chat(ctx, messages, options)
	require.NoError(t, err)
	require.EqualValues(t, 1, spy.calls.Load())
	changed := append([]chat.Message(nil), messages...)
	changed[1].Content = "unproven historical content"
	_, err = model.Chat(ctx, changed, options)
	require.ErrorIs(t, err, errPersonalExactDocument)
	_, err = model.Chat(ctx, messages, &chat.ChatOptions{MaxTokens: 200})
	require.ErrorIs(t, err, errPersonalExactDocument)
	wrongTenant := &legacyGuardedChat{inner: spy, gate: gate, tenant: 2}
	_, err = wrongTenant.Chat(ctx, messages, options)
	require.ErrorIs(t, err, errPersonalExactDocument)
	valid = false
	_, err = model.Chat(ctx, messages, options)
	require.ErrorIs(t, err, errPersonalExactDocument)
	require.EqualValues(t, 1, spy.calls.Load(), "no altered or revoked input may reach the provider")
	_, err = model.Chat(context.WithValue(context.Background(), forgedPersonalModelContextKey("exact_document_proof"), true), messages, options)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}

func TestExactPersonalModelRejectsUnprovenMediaAndToolInputs(t *testing.T) {
	for _, message := range []chat.Message{
		{Role: "user", Content: "text", Images: []string{"https://unproven.invalid/image"}},
		{Role: "tool", Content: "tool output", ToolCallID: "old-tool"},
		{Role: "assistant", Content: "text", ReasoningContent: "unknown history"},
		{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "unproven"}}},
	} {
		_, err := withExactPersonalMessages(context.Background(), 1, []chat.Message{message}, nil,
			func(context.Context) error { return nil })
		require.ErrorIs(t, err, errPersonalExactDocument)
	}
	_, err := withExactPersonalMessages(context.Background(), 1, []chat.Message{{Content: "text"}},
		&chat.ChatOptions{Tools: []chat.Tool{{Type: "function"}}}, func(context.Context) error { return nil })
	require.ErrorIs(t, err, errPersonalExactDocument)
}

func TestExactPersonalModelRerankBindsActualQueryPassagesAndAuthority(t *testing.T) {
	gate := &legacySpyGate{}
	gate.denied.Store(true)
	spy := &legacySpyModel{}
	model := &legacyGuardedRerank{Reranker: spy, gate: gate, tenant: 1}
	passages := []string{"verified first passage", "verified second passage"}
	valid := true
	ctx, err := withExactPersonalPassages(context.Background(), 1, "query", passages, func(context.Context) error {
		if !valid {
			return errPersonalExactDocument
		}
		return nil
	})
	require.NoError(t, err)
	_, err = model.Rerank(ctx, "query", passages)
	require.NoError(t, err)
	_, err = model.Rerank(ctx, "changed query", passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	_, err = model.Rerank(ctx, "query", []string{passages[0], "unknown cached passage"})
	require.ErrorIs(t, err, errPersonalExactDocument)
	other := &legacyGuardedRerank{Reranker: spy, gate: gate, tenant: 2}
	_, err = other.Rerank(ctx, "query", passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	valid = false
	_, err = model.Rerank(ctx, "query", passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	require.EqualValues(t, 1, spy.calls.Load())
	_, err = model.Rerank(context.WithValue(context.Background(), "exact_passages", true), "query", passages)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}

func TestExactPersonalModelRerankRejectsAuthorityLostDuringRequest(t *testing.T) {
	spy := &legacySpyModel{}
	gate := &legacySpyGate{}
	gate.denied.Store(true)
	model := &legacyGuardedRerank{Reranker: spy, gate: gate, tenant: 1}
	checks := 0
	ctx, err := withExactPersonalPassages(context.Background(), 1, "query", []string{"proved passage"},
		func(context.Context) error {
			checks++
			if checks >= 3 { // issuance, pre-request, then revoked at response
				return errPersonalExactDocument
			}
			return nil
		})
	require.NoError(t, err)
	result, err := model.Rerank(ctx, "query", []string{"proved passage"})
	require.ErrorIs(t, err, errPersonalExactDocument)
	require.Nil(t, result)
	require.EqualValues(t, 1, spy.calls.Load())
}

func TestExactPersonalModelPublishedRerankAdapterBindsActualCall(t *testing.T) {
	spy := &legacySpyModel{}
	gate := &legacySpyGate{}
	gate.denied.Store(true)
	valid := true
	ctx, err := types.WithExactPublishedDocumentRead(types.WithExecutionTenant(context.Background(), 1), 1,
		types.ExactDocumentScope{TenantID: 1, KnowledgeID: "document", KBID: "kb", Read: func(context.Context) error {
			if !valid {
				return errPersonalExactDocument
			}
			return nil
		}})
	require.NoError(t, err)
	adapter := &exactPublishedReranker{
		Reranker: &legacyGuardedRerank{Reranker: spy, gate: gate, tenant: 1},
		query:    "query", passages: []string{"canonical passage"}, tenant: 1,
	}
	_, err = adapter.Rerank(ctx, "query", []string{"canonical passage"})
	require.NoError(t, err)
	_, err = adapter.Rerank(ctx, "query", []string{"unproven cached text"})
	require.ErrorIs(t, err, errPersonalExactDocument)
	_, err = adapter.Rerank(ctx, "different query", adapter.passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	_, err = adapter.Rerank(context.Background(), "query", adapter.passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	valid = false
	_, err = adapter.Rerank(ctx, "query", adapter.passages)
	require.ErrorIs(t, err, errPersonalExactDocument)
	require.EqualValues(t, 1, spy.calls.Load())
}

func TestExactPersonalModelRecoveryInterlockIsNotPayloadAuthority(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", "")
	require.False(t, exactPersonalRecoveryAllows("original"))
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", " original , second ")
	require.True(t, exactPersonalRecoveryAllows("original"))
	require.True(t, exactPersonalRecoveryAllows("second"))
	require.False(t, exactPersonalRecoveryAllows("original-other"))
	require.False(t, exactPersonalRecoveryAllows(""))
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "0")
	require.False(t, exactPersonalRecoveryAllows("original"))
}
