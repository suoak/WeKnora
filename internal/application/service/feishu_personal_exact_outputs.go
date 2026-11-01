package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

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
