package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// Controlled failures intentionally do not wrap a SQL or vector driver cause.
var errAttemptCleanupFailed = errors.New("processing_cleanup_failed")

// cleanupAttemptChunks never targets a whole knowledge. IDs come from this
// invocation's newly constructed SQL chunks, including unindexed parent chunks.
// An old invocation therefore cannot remove a newer invocation's UUIDs.
// The vector callback must delete by these same ChunkIDs, not document IDs.
func (s *knowledgeService) cleanupAttemptChunks(ctx context.Context, knowledge *types.Knowledge, chunks []*types.Chunk, vectorCleanup func(context.Context, []string) error) error {
	if knowledge == nil || knowledge.TenantID == 0 || knowledge.ID == "" {
		return errAttemptCleanupFailed
	}
	ids := make([]string, 0, len(chunks))
	seen := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || chunk.ID == "" || chunk.TenantID != knowledge.TenantID || chunk.KnowledgeID != knowledge.ID || seen[chunk.ID] {
			return errAttemptCleanupFailed
		}
		seen[chunk.ID] = true
		ids = append(ids, chunk.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	// Cancellation may have ended the request, but cleanup has its own finite
	// lifetime. Preserve trusted context fields; never start an unbounded worker.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.chunkRepo.DeleteChunks(cleanupCtx, knowledge.TenantID, ids); err != nil {
		return errAttemptCleanupFailed
	}
	if vectorCleanup != nil {
		if err := vectorCleanup(cleanupCtx, ids); err != nil {
			return errAttemptCleanupFailed
		}
	}
	return nil
}
