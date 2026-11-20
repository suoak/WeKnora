package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type publishedSnapshotStore struct {
	DocumentPolicyStore
	canonical, published int
}

func (s *publishedSnapshotStore) LoadDocumentReadSnapshot(
	context.Context, string, uint64, string,
) (*types.EnterpriseReadSnapshot, error) {
	s.canonical++
	return &types.EnterpriseReadSnapshot{KBID: "canonical"}, nil
}

func (s *publishedSnapshotStore) LoadPublishedDocumentReadSnapshot(
	context.Context, string, uint64, string,
) (*types.EnterpriseReadSnapshot, error) {
	s.published++
	return &types.EnterpriseReadSnapshot{KBID: "published"}, nil
}

func TestPublishedSnapshotSelectionDoesNotChangeLegacyApprovalMode(t *testing.T) {
	s := &publishedSnapshotStore{}
	ctx := context.Background()
	legacy, err := loadPortalDocumentSnapshot(ctx, s, false, "user", 1, "kb")
	require.NoError(t, err)
	require.Equal(t, "canonical", legacy.KBID)
	published, err := loadPortalDocumentSnapshot(ctx, s, true, "user", 1, "kb")
	require.NoError(t, err)
	require.Equal(t, "published", published.KBID)
	require.Equal(t, 1, s.canonical)
	require.Equal(t, 1, s.published)
}
