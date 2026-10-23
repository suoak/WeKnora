package repository

import (
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyParentGraphP1(t *testing.T) {
	parent := &types.Chunk{
		ID:              "p",
		TenantID:        1,
		KnowledgeID:     "k",
		KnowledgeBaseID: "kb",
		ChunkType:       types.ChunkTypeParentText,
	}
	child := &types.Chunk{
		ID:              "c",
		TenantID:        1,
		KnowledgeID:     "k",
		KnowledgeBaseID: "kb",
		ChunkType:       types.ChunkTypeText,
		ParentChunkID:   "p",
	}
	got, err := legacyIndexedChunks([]*types.Chunk{parent, child})
	require.NoError(t, err)
	require.Equal(t, []*types.Chunk{child}, got)
	for _, tc := range []struct {
		name   string
		mutate func(*types.Chunk, *types.Chunk)
	}{
		{"old-parent", func(_, c *types.Chunk) { c.ParentChunkID = "old-version-parent" }},
		{"cross-tenant", func(p, _ *types.Chunk) { p.TenantID = 2 }},
		{"cross-document", func(p, _ *types.Chunk) { p.KnowledgeID = "other" }},
		{"nested-parent", func(p, c *types.Chunk) { p.ParentChunkID = c.ID }},
		{"old-adjacency", func(_, c *types.Chunk) { c.NextChunkID = "old-version-child" }},
		{"orphan-parent", func(_, c *types.Chunk) { c.ParentChunkID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c := *parent, *child
			tc.mutate(&p, &c)
			_, err := legacyIndexedChunks([]*types.Chunk{&p, &c})
			require.ErrorIs(t, err, ErrLegacyVersionFence)
		})
	}
	var diagnostic *LegacyFenceError
	require.True(t, errors.As(legacyFence("attempt_lease_expired"), &diagnostic))
	require.Equal(t, "attempt_lease_expired", diagnostic.Reason)
}
