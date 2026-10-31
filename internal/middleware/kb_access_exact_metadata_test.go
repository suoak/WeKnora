package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type exactMetadataLookup struct{ metadataCalls, bodyCalls int }

func (l *exactMetadataLookup) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	l.bodyCalls++
	return nil, types.ErrLegacySourceRestricted
}

func (l *exactMetadataLookup) GetKnowledgeBaseAccessMetadata(context.Context, string) (*types.KnowledgeBase, error) {
	l.metadataCalls++
	return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
}

func TestLookupKBAccessMetadataPreservesEditorSourceGate(t *testing.T) {
	lookup := &exactMetadataLookup{}
	row, err := LookupKBAccessMetadata(context.Background(), lookup, "kb", types.OrgRoleViewer)
	require.NoError(t, err)
	require.Equal(t, "kb", row.ID)
	require.Nil(t, row.GeneratedProfile)
	_, err = LookupKBAccessMetadata(context.Background(), lookup, "kb", types.OrgRoleEditor)
	require.True(t, errors.Is(err, types.ErrLegacySourceRestricted))
	require.Equal(t, 1, lookup.metadataCalls)
	require.Equal(t, 1, lookup.bodyCalls)
}
