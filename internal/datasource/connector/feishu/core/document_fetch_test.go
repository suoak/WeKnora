package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type documentFetchOps struct {
	checkpointFailureOps
	nodes   []string
	partial error
	items   []*types.FetchedItem
}

func (o *documentFetchOps) List(context.Context, *Client, string) ([]string, error, error) {
	o.lists++
	return o.nodes, o.partial, nil
}

func (o *documentFetchOps) Fetch(
	_ context.Context, _ *Client, node, root string, _ bool,
) ([]*types.FetchedItem, error) {
	o.fetches++
	if o.items != nil {
		return o.items, nil
	}
	return []*types.FetchedItem{{ExternalID: node, SourceResourceID: root, Content: []byte("complete document")}}, nil
}

func TestDocumentFetchTargetsOneDocumentAndRechecksBinding(t *testing.T) {
	ops := &documentFetchOps{nodes: []string{"sibling", "target", "later"}}
	config := &types.DataSourceConfig{ResourceIDs: []string{"configured-root", "other-root"}}
	item, err := FetchDocumentEngine(context.Background(), &Client{}, config, "configured-root", "target", ops)
	require.NoError(t, err)
	require.Equal(t, "target", item.ExternalID)
	require.Equal(t, 1, ops.fetches)
	require.Equal(t, []string{"configured-root", "other-root"}, config.ResourceIDs)
	fresh, err := item.CurrentSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, item.Content, fresh.Content)
	require.Equal(t, 2, ops.lists)
	ops.nodes = []string{"sibling", "later"}
	_, err = item.CurrentSnapshot(context.Background())
	require.Error(t, err)
	require.Equal(t, 2, ops.fetches, "a removed binding must not be fetched from a captured node")
}

func TestDocumentFetchRejectsUnselectedAmbiguousAndPartialSources(t *testing.T) {
	config := &types.DataSourceConfig{ResourceIDs: []string{"root"}}
	ops := &documentFetchOps{nodes: []string{"target"}}
	_, err := FetchDocumentEngine(context.Background(), &Client{}, config, "outside", "target", ops)
	require.Error(t, err)
	require.Zero(t, ops.lists)
	for _, nodes := range [][]string{nil, {"target", "target"}} {
		ops.nodes = nodes
		_, err = FetchDocumentEngine(context.Background(), &Client{}, config, "root", "target", ops)
		require.Error(t, err)
		require.Zero(t, ops.fetches)
	}
	ops.nodes = []string{"target"}
	ops.partial = errors.New("incomplete listing")
	_, err = FetchDocumentEngine(context.Background(), &Client{}, config, "root", "target", ops)
	require.ErrorIs(t, err, ops.partial)
	require.Zero(t, ops.fetches)
	ops.partial = nil
	for _, items := range [][]*types.FetchedItem{
		{{ExternalID: "target", SourceResourceID: "other-root"}},
		{{ExternalID: "target", SourceResourceID: "root", IsDeleted: true}},
		{{ExternalID: "target", SourceResourceID: "root"}, {ExternalID: "target", SourceResourceID: "root"}},
		{{ExternalID: "target", SourceResourceID: "root", Metadata: map[string]string{"attachment": "true"}}},
	} {
		ops.items = items
		_, err = FetchDocumentEngine(context.Background(), &Client{}, config, "root", "target", ops)
		require.Error(t, err)
	}
}
