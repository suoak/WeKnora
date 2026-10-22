package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type legacySpyGate struct{ denied atomic.Bool }

func (g *legacySpyGate) CheckLegacyModelInputs(context.Context, uint64) error {
	if g.denied.Load() {
		return types.ErrLegacySourceRestricted
	}
	return nil
}

type legacySpyModel struct {
	calls  atomic.Int64
	stream chan types.StreamResponse
}

func (s *legacySpyModel) GetModelID() string   { return "synthetic" }
func (s *legacySpyModel) GetModelName() string { return "synthetic" }
func (s *legacySpyModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	s.calls.Add(1)
	return &types.ChatResponse{}, nil
}
func (s *legacySpyModel) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	s.calls.Add(1)
	return s.stream, nil
}
func (s *legacySpyModel) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	s.calls.Add(1)
	return nil, nil
}
func TestIncrementalLegacyModelSpiesAndStreamingWithdrawal(t *testing.T) {
	gate := &legacySpyGate{}
	gate.denied.Store(true)
	spy := &legacySpyModel{stream: make(chan types.StreamResponse, 1)}
	model := &legacyGuardedChat{inner: spy, gate: gate, tenant: 42}
	_, err := model.Chat(context.Background(), []chat.Message{{Content: "restrictedcanary"}}, nil)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	rank := &legacyGuardedRerank{Reranker: spy, gate: gate, tenant: 42}
	_, err = rank.Rerank(context.Background(), "query", []string{"restrictedcanary"})
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	_, err = model.ChatStream(context.Background(), nil, nil)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Zero(t, spy.calls.Load(), "denied text must not enter external model client")
	gate.denied.Store(false)
	out, err := model.ChatStream(context.Background(), nil, nil)
	require.NoError(t, err)
	gate.denied.Store(true)
	spy.stream <- types.StreamResponse{}
	select {
	case _, ok := <-out:
		require.False(t, ok, "withdrawal closes stream before emitting next chunk")
	case <-time.After(time.Second):
		t.Fatal("stream did not close within bound")
	}
}
