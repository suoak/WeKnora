package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// LargeFeishuChunkLimit bounds explicit, checkpointed spreadsheet reparses.
const LargeFeishuChunkLimit = 100000

// LargeFeishuProgressWindows bounds successful continuations independently of failures.
const LargeFeishuProgressWindows = 128

// FeishuEmbeddingProgress is a private durable-work yield, never a publication receipt.
type FeishuEmbeddingProgress struct {
	Owner     *types.FeishuLegacyAttempt
	Completed int
	Total     int
}

func (p *FeishuEmbeddingProgress) Error() string {
	return fmt.Sprintf("Feishu embedding progress saved (%d/%d)", p.Completed, p.Total)
}

func largeCheckpointShape(c *types.FeishuLegacyEmbeddingCheckpoint) bool {
	return c != nil && c.Version == 2 && c.RequestID != "" && len(c.SourceConfig) == 64 &&
		len(c.Inputs) > 10000 && len(c.Inputs) <= LargeFeishuChunkLimit &&
		c.Dimension > 0 && c.Dimension <= 4096 &&
		int64(len(c.Inputs))*int64(c.Dimension)*4 <= 512<<20
}

// LargeSourceReparseRequest returns only the identity of a server-issued claim.
// Every persistence operation must additionally check the live SQL claim.
func LargeSourceReparseRequest(ctx context.Context) (string, string, bool) {
	claim, ok := ctx.Value(sourceDocumentReparseKey{}).(*sourceDocumentReparseClaim)
	if !ok || claim == nil || claim.request.FileType != "xlsx" ||
		claim.request.ProgressWindows >= LargeFeishuProgressWindows {
		return "", "", false
	}
	return claim.request.ID, claim.request.ConfigHash, true
}

func checkLargeCheckpointClaim(ctx context.Context, tx *gorm.DB, ds *types.DataSource,
	r *types.FeishuLegacyResource, c *types.FeishuLegacyEmbeddingCheckpoint,
) error {
	if c.Version == 1 {
		return nil
	}
	claim, err := checkSourceReparseClaim(ctx, tx, ds, r)
	if err != nil {
		return err
	}
	if !largeCheckpointShape(c) || claim == nil || claim.request.FileType != "xlsx" ||
		claim.request.ID != c.RequestID || claim.request.ConfigHash != c.SourceConfig ||
		claim.request.ProgressWindows >= LargeFeishuProgressWindows {
		return legacyFence("large_checkpoint_request_changed")
	}
	return nil
}

// YieldSourceDocumentReparse commits verified progress before releasing its private lease.
func (s *FeishuLegacyStore) YieldSourceDocumentReparse(ctx context.Context, ds *types.DataSource,
	progress *FeishuEmbeddingProgress,
) error {
	if progress == nil || progress.Owner == nil {
		return legacyFence("large_checkpoint_progress_missing")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, a, err := s.owned(ctx, tx, ds, progress.Owner)
		if err != nil {
			return err
		}
		c, err := embeddingCheckpoint(a)
		if err != nil {
			return err
		}
		if err = checkLargeCheckpointClaim(ctx, tx, ds, r, c); err != nil {
			return err
		}
		claim, err := checkSourceReparseClaim(ctx, tx, ds, r)
		if err != nil {
			return err
		}
		if claim == nil || c.Version != 2 {
			return legacyFence("large_checkpoint_request_missing")
		}
		var count int64
		if err = tx.Table("feishu_legacy_index_candidates").Where("attempt_id=?", a.ID).
			Count(&count).Error; err != nil {
			return err
		}
		request := claim.request
		if request.Attempts < 1 || request.ProgressWindows < 0 {
			return legacyFence("large_checkpoint_budget_invalid")
		}
		if count <= int64(request.CompletedChunks) || count >= int64(len(c.Inputs)) ||
			progress.Total != len(c.Inputs) {
			return legacyFence("large_checkpoint_progress_unconfirmed")
		}
		request.CompletedChunks, request.TotalChunks = int(count), len(c.Inputs)
		request.ProgressWindows++
		request.Attempts-- // Progress is not a failed attempt; failures retain their separate budget.
		request.ErrorCode = ""
		request.RetryAfter = time.Now().UTC().Add(time.Minute)
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		updated := tx.Model(&types.TaskPendingOp{}).Where("id=? AND op='processing'", claim.op.ID).
			Updates(map[string]any{"op": "queued", "payload": json.RawMessage(payload)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return legacyFence("large_checkpoint_progress_commit_lost")
		}
		// This attempt has durably imported every reusable input before yielding.
		// Drop only superseded private copies of this same request and snapshot,
		// avoiding quadratic storage growth across continuation generations.
		if err = tx.Exec(`DELETE FROM feishu_legacy_index_candidates i USING feishu_legacy_attempts old
WHERE i.attempt_id=old.id AND old.id<>? AND old.resource_id=? AND old.status='obsolete'
AND old.source_hash=? AND old.receipt->'embedding_checkpoint_v1'->>'request_id'=?
AND old.receipt->'embedding_checkpoint_v1'->>'model_stamp'=?
AND old.receipt->'embedding_checkpoint_v1'->>'kb_config_sha256'=?`, a.ID, r.ID, a.SourceHash,
			c.RequestID, c.ModelStamp, c.KBConfigSHA256).Error; err != nil {
			return err
		}
		return tx.Model(a).Update("lease_until", gorm.Expr("clock_timestamp()")).Error
	})
}
