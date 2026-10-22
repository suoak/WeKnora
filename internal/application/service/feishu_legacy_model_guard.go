package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
)

type legacyModelInputGate interface {
	CheckLegacyModelInputs(context.Context, uint64) error
}
type legacyGuardedChat struct {
	inner  chat.Chat
	gate   legacyModelInputGate
	tenant uint64
}

func (m *legacyGuardedChat) GetModelName() string { return m.inner.GetModelName() }
func (m *legacyGuardedChat) GetModelID() string   { return m.inner.GetModelID() }
func (m *legacyGuardedChat) Chat(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if e := m.gate.CheckLegacyModelInputs(ctx, m.tenant); e != nil {
		return nil, e
	}
	clean, err := sanitizeWikiGenerationMessages(ctx, msgs)
	if err != nil {
		return nil, err
	}
	return m.inner.Chat(ctx, clean, opts)
}
func (m *legacyGuardedChat) ChatStream(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	if e := m.gate.CheckLegacyModelInputs(ctx, m.tenant); e != nil {
		return nil, e
	}
	clean, cleanErr := sanitizeWikiGenerationMessages(ctx, msgs)
	if cleanErr != nil {
		return nil, cleanErr
	}
	request, cancel := context.WithCancel(ctx)
	in, err := m.inner.ChatStream(request, clean, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan types.StreamResponse)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-request.Done():
				return
			case response, ok := <-in:
				if !ok {
					return
				}
				if e := m.gate.CheckLegacyModelInputs(request, m.tenant); e != nil {
					return
				}
				select {
				case <-request.Done():
					return
				case out <- response:
				}
			}
		}
	}()
	return out, nil
}

type legacyGuardedRerank struct {
	rerank.Reranker
	gate   legacyModelInputGate
	tenant uint64
}

func (m *legacyGuardedRerank) MaxPassageRunes(q string) int {
	return rerank.MaxPassageRunes(m.Reranker, q)
}

func (m *legacyGuardedRerank) Rerank(ctx context.Context, q string, docs []string) ([]rerank.RankResult, error) {
	if e := m.gate.CheckLegacyModelInputs(ctx, m.tenant); e != nil {
		return nil, e
	}
	return m.Reranker.Rerank(ctx, q, docs)
}
