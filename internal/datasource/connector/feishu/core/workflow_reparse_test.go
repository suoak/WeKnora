package core

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type nestedReparseHandler struct {
	client     *Client
	config     *types.DataSourceConfig
	checkpoint bool
	called     bool
}

func (h *nestedReparseHandler) reparse(ctx context.Context) error {
	if h.called {
		return nil
	}
	h.called = true
	ops := &documentFetchOps{nodes: []string{"target"}}
	item, err := FetchDocumentEngine(ctx, h.client, h.config, "root", "target", ops)
	if err != nil {
		return err
	}
	_, err = item.CurrentSnapshot(ctx)
	return err
}

func (h *nestedReparseHandler) Emit(ctx context.Context, _ types.FetchedItem) error {
	if !h.checkpoint {
		return h.reparse(ctx)
	}
	return nil
}

func (h *nestedReparseHandler) Checkpoint(ctx context.Context, _ *types.SyncCursor) error {
	if h.checkpoint {
		return h.reparse(ctx)
	}
	return nil
}

func TestWorkflowStreamCanReparseDocumentFromCallback(t *testing.T) {
	interval := FeishuStreamCheckpointInterval
	FeishuStreamCheckpointInterval = 1
	t.Cleanup(func() { FeishuStreamCheckpointInterval = interval })
	for _, checkpoint := range []bool{false, true} {
		name := "emit"
		if checkpoint {
			name = "checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			p := &appPolicy{workflowGate: make(chan struct{}, 1)}
			client := &Client{policy: p}
			config := &types.DataSourceConfig{ResourceIDs: []string{"root"}}
			h := &nestedReparseHandler{client: client, config: config, checkpoint: checkpoint}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := runSync(ctx, client, config, config.ResourceIDs, nil, h,
				&documentFetchOps{nodes: []string{"target"}})
			require.NoError(t, err, "a checkpoint must not wait for a workflow slot already held by its own stream")
			require.True(t, h.called)
			require.Empty(t, p.workflowGate, "the outer workflow must release its slot")
		})
	}
}
