package core

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type moveOps struct {
	checkpointFailureOps
	previous map[string]map[string]string
}

func (o *moveOps) List(_ context.Context, _ *Client, root string) ([]string, error, error) {
	if root == "old-root" {
		return nil, nil, nil
	}
	return []string{"moved-document"}, nil, nil
}
func (o *moveOps) DecodeCursorTimes(map[string]interface{}) map[string]map[string]string {
	return o.previous
}
func (o *moveOps) EncodeCursor(times map[string]map[string]string, _ time.Time) *types.SyncCursor {
	copy := map[string]interface{}{}
	for root, rows := range times {
		entries := map[string]string{}
		for token, edited := range rows {
			entries[token] = edited
		}
		copy[root] = entries
	}
	return &types.SyncCursor{ConnectorCursor: copy}
}

type moveHandler struct {
	items       []types.FetchedItem
	checkpoints []*types.SyncCursor
}

func (h *moveHandler) Emit(_ context.Context, item types.FetchedItem) error {
	h.items = append(h.items, item)
	return nil
}
func (h *moveHandler) Checkpoint(_ context.Context, c *types.SyncCursor) error {
	h.checkpoints = append(h.checkpoints, c)
	return nil
}
func TestIncrementalMoveWithinConfiguredRootsDoesNotDelete(t *testing.T) {
	before := FeishuStreamCheckpointInterval
	FeishuStreamCheckpointInterval = 1
	t.Cleanup(func() { FeishuStreamCheckpointInterval = before })
	ops := &moveOps{previous: map[string]map[string]string{"old-root": {"moved-document": "old"}}}
	h := &moveHandler{}
	final, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"old-root", "new-root"}, &types.SyncCursor{ConnectorCursor: map[string]interface{}{}}, h, ops)
	require.NoError(t, err)
	for _, item := range h.items {
		require.False(t, item.IsDeleted, "moving within authorized roots must not revoke the stable source")
	}
	require.NotEmpty(t, h.checkpoints)
	require.Contains(t, h.checkpoints[0].ConnectorCursor["old-root"], "moved-document", "a checkpoint must retain not-yet-reconciled removal")
	require.Empty(t, final.ConnectorCursor["old-root"])
	require.Contains(t, final.ConnectorCursor["new-root"], "moved-document")
}
