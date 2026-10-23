package repository

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

// legacyIndexedChunks validates a complete, self-contained version graph.
// Parent context is persisted with its children, but is never embedded/indexed.
func legacyIndexedChunks(chunks []*types.Chunk) ([]*types.Chunk, error) {
	byID := make(map[string]*types.Chunk, len(chunks))
	indexed := make([]*types.Chunk, 0, len(chunks))
	for _, c := range chunks {
		if c == nil || c.ID == "" || byID[c.ID] != nil {
			return nil, legacyFence("invalid_chunk_identity")
		}
		if c.ChunkType != types.ChunkTypeText && c.ChunkType != types.ChunkTypeParentText {
			return nil, legacyFence("unsupported_chunk_kind")
		}
		byID[c.ID] = c
		if c.ChunkType == types.ChunkTypeText {
			indexed = append(indexed, c)
		}
	}
	children := map[string]int{}
	for _, c := range chunks {
		if c.ParentChunkID != "" {
			p := byID[c.ParentChunkID]
			if c.ChunkType != types.ChunkTypeText || p == nil || p.ChunkType != types.ChunkTypeParentText || p.ParentChunkID != "" || p.KnowledgeID != c.KnowledgeID || p.KnowledgeBaseID != c.KnowledgeBaseID || p.TenantID != c.TenantID {
				return nil, legacyFence("cross_version_parent")
			}
			children[p.ID]++
		}
		for _, id := range []string{c.PreChunkID, c.NextChunkID} {
			if id != "" {
				n := byID[id]
				if n == nil || n.ID == c.ID || n.ChunkType != c.ChunkType || n.KnowledgeID != c.KnowledgeID || n.KnowledgeBaseID != c.KnowledgeBaseID || n.TenantID != c.TenantID {
					return nil, legacyFence("cross_version_adjacency")
				}
			}
		}
	}
	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeParentText && children[c.ID] == 0 {
			return nil, legacyFence("orphan_parent")
		}
	}
	if len(indexed) == 0 {
		return nil, legacyFence("no_retrievable_chunks")
	}
	return indexed, nil
}

// LegacyFenceError provides a stable structured diagnostic without text,
// credentials, URLs, configuration, owner nonces or content digests.
type LegacyFenceError struct{ Reason string }

func (e *LegacyFenceError) Error() string {
	return fmt.Sprintf("%s (reason=%s)", ErrLegacyVersionFence, e.Reason)
}
func (e *LegacyFenceError) Unwrap() error { return ErrLegacyVersionFence }
func legacyFence(reason string) error     { return &LegacyFenceError{Reason: reason} }
