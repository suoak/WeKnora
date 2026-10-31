package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func exactDerivedDigest(summary string, profile *types.KnowledgeProfile) ([32]byte, error) {
	data, err := json.Marshal(struct {
		Summary string
		Profile *types.KnowledgeProfile
	}{summary, profile})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func (c *exactPersonalConsumer) summaryMessages(
	ctx context.Context, knowledge *types.Knowledge, chunks []*types.Chunk,
	messages []chat.Message, options *chat.ChatOptions,
) (context.Context, error) {
	if !c.indexed || knowledge == nil || knowledge.ID != c.source.knowledge ||
		knowledge.TenantID != c.source.tenant || knowledge.KnowledgeBaseID != c.source.kb ||
		knowledge.CustomMetadataText() != "" || len(chunks) == 0 {
		return ctx, errPersonalExactDocument
	}
	if err := c.verifyParserChunks(chunks); err != nil {
		return ctx, err
	}
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	live := func(current context.Context) error {
		if _, err := c.recovery.revalidateExactPersonalDocument(current, c.source); err != nil {
			return err
		}
		ks := c.recovery.service.knowledge.(*knowledgeService)
		rows, err := ks.chunkRepo.ListChunksByID(current, c.source.tenant, ids)
		if err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return errPersonalExactDocument
		}
		return c.verifyParserChunks(rows)
	}
	return withExactPersonalMessages(ctx, c.source.tenant, messages, options, live)
}

func (c *exactPersonalConsumer) acceptSummaryChunks(knowledge *types.Knowledge, chunks []*types.Chunk) error {
	if !c.derivedReady || knowledge == nil {
		return errPersonalExactDocument
	}
	digest, err := exactDerivedDigest(knowledge.Description, knowledge.Profile)
	if err != nil || digest != c.derived {
		return errPersonalExactDocument
	}
	content := buildSummaryChunkContent(knowledge.Description, knowledge.Profile)
	for _, chunk := range chunks {
		if chunk == nil || chunk.KnowledgeID != c.source.knowledge || chunk.TenantID != c.source.tenant ||
			chunk.KnowledgeBaseID != c.source.kb || chunk.ChunkType != types.ChunkTypeSummary ||
			chunk.Content != content || chunk.ContentRevision != 0 {
			return errPersonalExactDocument
		}
		value, err := exactChunkDigest(chunk)
		if err != nil {
			return err
		}
		c.outputs[chunk.ID] = value
	}
	return nil
}
