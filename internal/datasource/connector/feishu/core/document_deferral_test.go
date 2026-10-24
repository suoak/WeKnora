package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type deferralTestOps struct {
	checkpointFailureOps
	nodes   []string
	fetched []string
	fail    string
}

func (o *deferralTestOps) List(context.Context, *Client, string) ([]string, error, error) {
	o.lists++
	return o.nodes, nil, nil
}

func (o *deferralTestOps) Fetch(_ context.Context, _ *Client, node, root string, _ bool) ([]*types.FetchedItem, error) {
	o.fetched = append(o.fetched, node)
	if node == o.fail {
		return nil, ErrPermission
	}
	return []*types.FetchedItem{{ExternalID: node, SourceResourceID: root, Content: []byte("accepted body")}}, nil
}

func (*deferralTestOps) DecodeCursorTimes(m map[string]interface{}) map[string]map[string]string {
	b, _ := json.Marshal(m)
	var c FeishuCursor
	_ = json.Unmarshal(b, &c)
	return c.SpaceNodeTimes
}

func (*deferralTestOps) EncodeCursor(m map[string]map[string]string, t time.Time) *types.SyncCursor {
	b, _ := json.Marshal(FeishuCursor{SpaceNodeTimes: m, LastSyncTime: t})
	var v map[string]interface{}
	_ = json.Unmarshal(b, &v)
	return &types.SyncCursor{ConnectorCursor: v}
}

type deferralTestHandler struct {
	policy        []types.SyncDocumentDeferral
	items         []types.FetchedItem
	checkpoints   []*types.SyncCursor
	checkpointErr error
}

func (h *deferralTestHandler) DocumentDeferrals(context.Context) ([]types.SyncDocumentDeferral, error) {
	return h.policy, nil
}

func (h *deferralTestHandler) Emit(_ context.Context, item types.FetchedItem) error {
	if item.Metadata["error"] != "" {
		return errors.New("unaccepted document")
	}
	h.items = append(h.items, item)
	return nil
}

func (h *deferralTestHandler) Checkpoint(_ context.Context, c *types.SyncCursor) error {
	if h.checkpointErr != nil {
		return h.checkpointErr
	}
	b, _ := json.Marshal(c)
	var copy types.SyncCursor
	_ = json.Unmarshal(b, &copy)
	h.checkpoints = append(h.checkpoints, &copy)
	return nil
}

func deferralEntry() types.SyncDocumentDeferral {
	return types.SyncDocumentDeferral{
		SourceResourceID: "root",
		ExternalID:       "bad",
		ReasonCode:       types.DocumentDeferralAwaitingAuthorization,
	}
}

func TestDocumentDeferralSharedEngine(t *testing.T) {
	ctx := context.Background()
	ops := &deferralTestOps{nodes: []string{"bad", "good"}, fail: "bad"}
	h := &deferralTestHandler{policy: []types.SyncDocumentDeferral{deferralEntry()}}
	initial := ops.EncodeCursor(map[string]map[string]string{"root": {"bad": "new", "good": "old"}}, time.Now())
	next, err := runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, initial, h, ops)
	require.NoError(t, err)
	require.Equal(t, []string{"good"}, ops.fetched)
	require.Len(t, h.items, 1)
	require.Len(t, h.checkpoints, 2)
	require.Equal(t, "new", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
	require.Contains(t, next.ConnectorCursor, pendingDocumentCursorKey)
	// Restart after authorization: the timestamp is unchanged, but pending work
	// must be re-fetched without clearing the accepted cursor.
	b, err := json.Marshal(next)
	require.NoError(t, err)
	var restarted types.SyncCursor
	require.NoError(t, json.Unmarshal(b, &restarted))
	ops2 := &deferralTestOps{nodes: []string{"bad", "good"}}
	h2 := &deferralTestHandler{}
	final, err := runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, &restarted, h2, ops2)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, ops2.fetched)
	require.NotContains(t, final.ConnectorCursor, pendingDocumentCursorKey)
}

func TestDocumentDeferralDoesNotIgnoreUnknownFailure(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "unknown", "later"}, fail: "unknown"}
	h := &deferralTestHandler{policy: []types.SyncDocumentDeferral{deferralEntry()}}
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.Error(t, err)
	require.Nil(t, next)
	require.Equal(t, []string{"unknown"}, ops.fetched)
	require.Len(t, h.checkpoints, 1)
}

func TestDocumentDeferralCheckpointAndScopeFailClosed(t *testing.T) {
	for _, kind := range []string{"checkpoint", "scope", "corrupt_cursor"} {
		t.Run(kind, func(t *testing.T) {
			ops := &deferralTestOps{nodes: []string{"bad", "good"}}
			h := &deferralTestHandler{policy: []types.SyncDocumentDeferral{deferralEntry()}}
			var cursor *types.SyncCursor
			switch kind {
			case "checkpoint":
				h.checkpointErr = types.ErrStaleSyncExecution
			case "scope":
				h.policy[0].SourceResourceID = "other"
			case "corrupt_cursor":
				cursor = &types.SyncCursor{ConnectorCursor: map[string]interface{}{pendingDocumentCursorKey: "invalid"}}
			}
			next,
				err := runSync(context.Background(),
				&Client{},
				&types.DataSourceConfig{},
				[]string{"root"},
				cursor,
				h,
				ops)
			require.Error(t, err)
			require.Nil(t, next)
			require.Zero(t, ops.lists)
			require.Empty(t, ops.fetched)
		})
	}
}

func TestDocumentDeferralMissingDocumentIsNotDeleted(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"good"}}
	h := &deferralTestHandler{policy: []types.SyncDocumentDeferral{deferralEntry()}}
	initial := ops.EncodeCursor(map[string]map[string]string{"root": {"bad": "old", "gone": "old"}}, time.Now())
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, initial, h, ops)
	require.NoError(t, err)
	require.Equal(t, "old", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
	for _, item := range h.items {
		require.NotEqual(t, "bad", item.ExternalID)
	}
	require.True(t, h.items[len(h.items)-1].IsDeleted)
}

func TestDocumentDeferralOverlappingRootsDoNotFetchDeferredIdentity(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "good"}, fail: "bad"}
	h := &deferralTestHandler{policy: []types.SyncDocumentDeferral{deferralEntry()}}
	_,
		err := runSync(context.Background(),
		&Client{},
		&types.DataSourceConfig{},
		[]string{
			"root",
			"overlapping",
		},
		nil,
		h,
		ops)
	require.NoError(t, err)
	for _, id := range ops.fetched {
		require.NotEqual(t, "bad", id)
	}
}
