package core

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLatestSnapshotRefetchesBindingAndRespectsPermission(t *testing.T) {
	t.Setenv("KNOWHUB_FEISHU_LATEST_CONTENT", "true")
	ops := &deferralTestOps{nodes: []string{"document"}}
	h := &deferralTestHandler{}
	_, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.NoError(t, err)
	require.Len(t, h.items, 1)
	require.Equal(t, 1, ops.lists, "normal emission adds no eager re-list")
	callback := h.items[0].CurrentSnapshot
	require.NotNil(t, callback)
	fresh, err := callback(context.Background())
	require.NoError(t, err)
	require.Equal(t, "document", fresh.ExternalID)
	require.Equal(t, 2, ops.lists)
	ops.fail = "document"
	_, err = callback(context.Background())
	require.ErrorIs(t, err, ErrPermission)
	ops.nodes = nil
	_, err = callback(context.Background())
	require.Error(t, err, "a removed source binding must not reuse the old node")
}

func TestLatestSnapshotDisabledLeavesOrdinaryFetchUnchanged(t *testing.T) {
	t.Setenv("KNOWHUB_FEISHU_LATEST_CONTENT", "false")
	ops := &deferralTestOps{nodes: []string{"document"}}
	h := &deferralTestHandler{}
	_, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.NoError(t, err)
	require.Len(t, h.items, 1)
	require.Nil(t, h.items[0].CurrentSnapshot)
	require.Equal(t, 1, ops.lists)
}
