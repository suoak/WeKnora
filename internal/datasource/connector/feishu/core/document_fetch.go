package core

import (
	"context"
	"errors"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
)

// FetchDocumentEngine retrieves exactly one configured source document. It
// never changes the source selection or advances a traversal/deletion cursor.
// Every snapshot check re-lists the binding instead of reusing a captured node.
func FetchDocumentEngine[N any](
	ctx context.Context, client *Client, config *types.DataSourceConfig,
	root, externalID string, ops NodeOps[N],
) (*types.FetchedItem, error) {
	if config == nil || root == "" || externalID == "" || !slices.Contains(config.ResourceIDs, root) {
		return nil, errors.New("document is outside configured source selection")
	}
	client.ensureRuntime()
	release, err := client.policy.acquireWorkflow(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	fetch := func(check context.Context) (*types.FetchedItem, error) {
		nodes, partial, err := ops.List(check, client, root)
		if err != nil {
			return nil, err
		}
		if partial != nil {
			return nil, partial
		}
		var selected N
		matches := 0
		for _, node := range nodes {
			if ops.Token(node) == externalID {
				selected = node
				matches++
			}
		}
		if matches != 1 {
			return nil, errors.New("current source binding unavailable or ambiguous")
		}
		items, err := ops.Fetch(check, client, selected, root, config.MultimodalEnabled)
		if err != nil {
			return nil, err
		}
		var found *types.FetchedItem
		for _, item := range items {
			if item == nil || item.ExternalID != externalID || item.Metadata["attachment"] == "true" {
				continue
			}
			if found != nil || item.IsDeleted || item.SourceResourceID != root {
				return nil, errors.New("current source document identity unconfirmed")
			}
			found = item
		}
		if found == nil {
			return nil, errors.New("current source document unavailable")
		}
		return found, nil
	}
	item, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	item.CurrentSnapshot = fetch
	return item, nil
}
