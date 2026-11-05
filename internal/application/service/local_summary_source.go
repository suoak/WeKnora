package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type localSummarySourceReader interface {
	LocalSummarySourceIdentity(context.Context, uint64, string, string, string) (string, bool, error)
}

// Reuse the existing message-digest/live-check model boundary for this exact
// local document. No tenant-wide permission is inferred from a chunk ID.
// Personal imports and connector documents retain their own source protocols.
func (s *knowledgeService) localSummaryMessages(
	ctx context.Context, knowledge *types.Knowledge, chunks []*types.Chunk,
	messages []chat.Message, options *chat.ChatOptions,
) (context.Context, error) {
	reader, ok := s.repo.(localSummarySourceReader)
	if !ok || s.DocumentACLReadEnabled() || knowledge == nil {
		return ctx, nil
	}
	metadata := knowledge.CustomMetadataText()
	identity, handled, err := reader.LocalSummarySourceIdentity(
		ctx, knowledge.TenantID, knowledge.KnowledgeBaseID, knowledge.ID, metadata,
	)
	if err != nil || !handled {
		return ctx, err
	}
	if identity == "" || len(chunks) == 0 || len(chunks) > 20000 {
		return ctx, types.ErrLegacySourceRestricted
	}
	ids := make([]string, 0, len(chunks))
	expected := make(map[string][32]byte, len(chunks))
	for _, chunk := range chunks {
		if !localSummaryChunkMatches(knowledge, chunk) {
			return ctx, types.ErrLegacySourceRestricted
		}
		if _, duplicate := expected[chunk.ID]; duplicate {
			return ctx, types.ErrLegacySourceRestricted
		}
		ids = append(ids, chunk.ID)
		digest, err := localSummaryChunkDigest(chunk)
		if err != nil {
			return ctx, err
		}
		expected[chunk.ID] = digest
	}
	live := func(current context.Context) error {
		fresh, handled, err := reader.LocalSummarySourceIdentity(
			current, knowledge.TenantID, knowledge.KnowledgeBaseID, knowledge.ID, metadata,
		)
		if err != nil {
			return err
		}
		if !handled || fresh != identity {
			return ErrSummaryRefreshStale
		}
		rows, err := s.chunkRepo.ListChunksByID(current, knowledge.TenantID, ids)
		if err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return ErrSummaryRefreshStale
		}
		seen := make(map[string]bool, len(rows))
		for _, row := range rows {
			if !localSummaryChunkMatches(knowledge, row) || seen[row.ID] {
				return ErrSummaryRefreshStale
			}
			digest, err := localSummaryChunkDigest(row)
			if err != nil {
				return err
			}
			if digest != expected[row.ID] {
				return ErrSummaryRefreshStale
			}
			seen[row.ID] = true
		}
		fresh, handled, err = reader.LocalSummarySourceIdentity(
			current, knowledge.TenantID, knowledge.KnowledgeBaseID, knowledge.ID, metadata,
		)
		if err != nil {
			return err
		}
		if !handled || fresh != identity {
			return ErrSummaryRefreshStale
		}
		return nil
	}
	return withExactPersonalMessages(ctx, knowledge.TenantID, messages, options, live)
}

func localSummaryChunkMatches(knowledge *types.Knowledge, chunk *types.Chunk) bool {
	return chunk != nil && chunk.ID != "" && chunk.TenantID == knowledge.TenantID &&
		chunk.KnowledgeID == knowledge.ID && chunk.KnowledgeBaseID == knowledge.KnowledgeBaseID &&
		chunk.IsEnabled && !chunk.DeletedAt.Valid && chunk.ChunkType == types.ChunkTypeText
}

func localSummaryChunkDigest(chunk *types.Chunk) ([32]byte, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}
