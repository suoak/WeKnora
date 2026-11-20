package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Only published-default evaluation can use epoch fences. Legacy approvals
// continue comparing their original canonical hashes. Stores without this
// optimization retain their existing snapshot implementation.
func loadPortalDocumentSnapshot(
	ctx context.Context, store DocumentPolicyStore, published bool, user string, key uint64, kb string,
) (*types.EnterpriseReadSnapshot, error) {
	if published {
		if live, ok := store.(interface {
			LoadPublishedDocumentReadSnapshot(
				context.Context, string, uint64, string,
			) (*types.EnterpriseReadSnapshot, error)
		}); ok {
			return live.LoadPublishedDocumentReadSnapshot(ctx, user, key, kb)
		}
	}
	return store.LoadDocumentReadSnapshot(ctx, user, key, kb)
}
