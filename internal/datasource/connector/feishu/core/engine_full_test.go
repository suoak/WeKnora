package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fullStreamOps struct {
	moveOps
	partial error
}

func (o *fullStreamOps) List(context.Context, *Client, string) ([]string, error, error) {
	o.lists++
	return []string{"document"}, o.partial, nil
}

func TestFullStreamRefetchesUnchangedAndRetainsDeletionBaseline(t *testing.T) {
	for _, full := range []bool{false, true} {
		ops := &fullStreamOps{moveOps: moveOps{
			previous: map[string]map[string]string{"root": {"document": "new", "removed": "old"}},
		}}
		h := &moveHandler{}
		config := &types.DataSourceConfig{ResourceIDs: []string{"root"}}
		baseline := &types.SyncCursor{ConnectorCursor: map[string]interface{}{}}
		var next *types.SyncCursor
		var err error
		if full {
			next, err = FetchFullStreamEngine(context.Background(), &Client{}, config, baseline, h, ops)
		} else {
			next, err = FetchStreamEngine(context.Background(), &Client{}, config, baseline, h, ops)
		}
		require.NoError(t, err)
		wantFetches := 0
		if full {
			wantFetches = 1
		}
		require.Equal(t, wantFetches, ops.fetches)
		deletions := 0
		for _, item := range h.items {
			if item.IsDeleted {
				require.Equal(t, "removed", item.ExternalID)
				deletions++
			}
		}
		require.Equal(t, 1, deletions, "full read must not erase the previous deletion baseline")
		require.NotContains(t, next.ConnectorCursor["root"], "removed")
	}
}

func TestFullStreamPartialListingCannotDeleteUnlistedHistory(t *testing.T) {
	ops := &fullStreamOps{moveOps: moveOps{
		previous: map[string]map[string]string{"root": {"document": "new", "unlisted": "old"}},
	}, partial: errors.New("synthetic incomplete listing")}
	h := &moveHandler{}
	next, err := FetchFullStreamEngine(context.Background(), &Client{},
		&types.DataSourceConfig{ResourceIDs: []string{"root"}},
		&types.SyncCursor{ConnectorCursor: map[string]interface{}{}}, h, ops)
	require.NoError(t, err)
	require.Equal(t, 1, ops.fetches)
	require.Contains(t, next.ConnectorCursor["root"], "unlisted")
	for _, item := range h.items {
		require.False(t, item.IsDeleted)
	}
}

func TestFullStreamCheckpointFailureStopsBeforeNextResource(t *testing.T) {
	before := FeishuStreamCheckpointInterval
	FeishuStreamCheckpointInterval = 1
	t.Cleanup(func() { FeishuStreamCheckpointInterval = before })
	ops := &fullStreamOps{moveOps: moveOps{
		previous: map[string]map[string]string{"first": {"document": "new"}},
	}}
	h := &failedCheckpointHandler{err: types.ErrStaleSyncExecution}
	next, err := FetchFullStreamEngine(context.Background(), &Client{},
		&types.DataSourceConfig{ResourceIDs: []string{"first", "must-not-run"}},
		&types.SyncCursor{ConnectorCursor: map[string]interface{}{}}, h, ops)
	require.ErrorIs(t, err, types.ErrStaleSyncExecution)
	require.Nil(t, next)
	require.Equal(t, 1, ops.lists)
	require.Equal(t, 1, ops.fetches)
}
