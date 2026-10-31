package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func exactChunkDigest(chunk *types.Chunk) ([32]byte, error) {
	if chunk == nil {
		return [32]byte{}, errPersonalExactDocument
	}
	canonicalChunk := *chunk
	// Database precision/sequence allocation and staging visibility do not
	// change parser content. Every content, relation, locator, metadata and
	// editing-revision field remains part of the canonical digest.
	canonicalChunk.CreatedAt, canonicalChunk.UpdatedAt = time.Time{}, time.Time{}
	canonicalChunk.SeqID = 0
	canonicalChunk.IsEnabled = false
	data, err := json.Marshal(struct {
		Chunk         types.Chunk
		SourceContent string
	}{canonicalChunk, canonicalChunk.SourceContent})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func (c *exactPersonalConsumer) captureParserChunks(ctx context.Context, chunks []*types.Chunk) error {
	if c.parsed == nil || c.parsed.source == nil || c.parsed.source.fileSHA256 != c.source.fileSHA256 {
		return errPersonalExactDocument
	}
	if _, err := c.recovery.revalidateExactPersonalDocument(ctx, c.source); err != nil {
		return err
	}
	outputs := make(map[string][32]byte, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || chunk.ID == "" || chunk.TenantID != c.source.tenant ||
			chunk.KnowledgeID != c.source.knowledge || chunk.KnowledgeBaseID != c.source.kb ||
			chunk.ContentRevision != 0 {
			return errPersonalExactDocument
		}
		if _, exists := outputs[chunk.ID]; exists {
			return errPersonalExactDocument
		}
		digest, err := exactChunkDigest(chunk)
		if err != nil {
			return err
		}
		outputs[chunk.ID] = digest
	}
	c.outputs = outputs
	return nil
}

func (c *exactPersonalConsumer) verifyParserChunks(chunks []*types.Chunk) error {
	if len(c.outputs) == 0 {
		return errPersonalExactDocument
	}
	for _, chunk := range chunks {
		if chunk == nil {
			return errPersonalExactDocument
		}
		want, exists := c.outputs[chunk.ID]
		got, err := exactChunkDigest(chunk)
		if !exists || err != nil || got != want {
			return errPersonalExactDocument
		}
	}
	return nil
}
