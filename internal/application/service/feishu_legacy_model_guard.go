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

func (m *legacyGuardedChat) checkInputs(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) error {
	if exact, err := checkExactPersonalMessages(ctx, m.tenant, msgs, opts); exact {
		return err
	}
	return m.gate.CheckLegacyModelInputs(ctx, m.tenant)
}

func (m *legacyGuardedChat) Chat(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if e := m.checkInputs(ctx, msgs, opts); e != nil {
		return nil, e
	}
	clean, err := sanitizeWikiGenerationMessages(ctx, msgs)
	if err != nil {
		return nil, err
	}
	if exact, checkErr := checkExactPersonalMessages(ctx, m.tenant, clean, opts); exact && checkErr != nil {
		return nil, checkErr
	}
	response, err := m.inner.Chat(ctx, clean, opts)
	if err != nil {
		return nil, err
	}
	if exact, checkErr := checkExactPersonalMessages(ctx, m.tenant, clean, opts); exact && checkErr != nil {
		return nil, checkErr
	}
	return response, nil
}

func (m *legacyGuardedChat) ChatStream(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	if e := m.checkInputs(ctx, msgs, opts); e != nil {
		return nil, e
	}
	clean, cleanErr := sanitizeWikiGenerationMessages(ctx, msgs)
	if cleanErr != nil {
		return nil, cleanErr
	}
	if exact, checkErr := checkExactPersonalMessages(ctx, m.tenant, clean, opts); exact && checkErr != nil {
		return nil, checkErr
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
				if e := m.checkInputs(request, clean, opts); e != nil {
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
	exact, err := checkExactPersonalPassages(ctx, m.tenant, q, docs)
	if exact {
		if err != nil {
			return nil, err
		}
	} else if err = m.gate.CheckLegacyModelInputs(ctx, m.tenant); err != nil {
		return nil, err
	}
	results, err := m.Reranker.Rerank(ctx, q, docs)
	if err != nil {
		return nil, err
	}
	if exact {
		if _, err = checkExactPersonalPassages(ctx, m.tenant, q, docs); err != nil {
			return nil, err
		}
	}
	return results, nil
}
