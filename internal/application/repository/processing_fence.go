package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrProcessingFence is deliberately independent of driver error text.
var ErrProcessingFence = errors.New("processing execution is no longer current")

// ProcessingFence is an internal capability. It must never be exposed as an API credential.
type ProcessingFence struct {
	TenantID    uint64 `gorm:"primaryKey"`
	KnowledgeID string `gorm:"primaryKey"`
	Generation  int64
	ExecutionID string
	LeaseUntil  time.Time
}

func (ProcessingFence) TableName() string { return "knowledge_processing_executions" }

type processingOwnedChunk struct {
	TenantID        uint64 `gorm:"primaryKey"`
	KnowledgeID     string `gorm:"primaryKey"`
	Generation      int64  `gorm:"primaryKey"`
	ExecutionID     string
	ChunkID         string `gorm:"primaryKey"`
	CleanupComplete bool
}

func (processingOwnedChunk) TableName() string { return "knowledge_processing_owned_chunks" }

// ProcessingFenceRepository is the SQL boundary for the next processing protocol.
// All generation allocators and state writers must use the same knowledge-row lock.
// It is intentionally not wired into the legacy worker until every writer is migrated.
type ProcessingFenceRepository struct{ db *gorm.DB }

func NewProcessingFenceRepository(db *gorm.DB) *ProcessingFenceRepository {
	return &ProcessingFenceRepository{db: db}
}

func lockProcessingKnowledge(tx *gorm.DB, tenantID uint64, knowledgeID string) error {
	var row struct {
		ID          string
		ParseStatus string
	}
	if err := tx.Table("knowledges").Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, knowledgeID).
		Take(&row).Error; err != nil {
		return err
	}
	if row.ParseStatus == types.ParseStatusCancelled || row.ParseStatus == types.ParseStatusDeleting {
		return ErrProcessingFence
	}
	return nil
}

func (r *ProcessingFenceRepository) Allocate(ctx context.Context, tenantID uint64, knowledgeID string, lease time.Duration) (ProcessingFence, error) {
	var next ProcessingFence
	if tenantID == 0 || knowledgeID == "" || lease <= 0 || lease > time.Hour {
		return next, ErrProcessingFence
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockProcessingKnowledge(tx, tenantID, knowledgeID); err != nil {
			return err
		}
		var current ProcessingFence
		err := tx.Where("tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID).Take(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		next = ProcessingFence{TenantID: tenantID, KnowledgeID: knowledgeID, Generation: current.Generation + 1, ExecutionID: uuid.NewString()}
		// DB time is authoritative; worker clocks cannot extend an expired capability.
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&now); err != nil {
			return err
		}
		next.LeaseUntil = now.Add(lease)
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "knowledge_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"generation", "execution_id", "lease_until"})}).Create(&next).Error
	})
	return next, err
}

func checkProcessingFence(tx *gorm.DB, fence ProcessingFence) error {
	if err := lockProcessingKnowledge(tx, fence.TenantID, fence.KnowledgeID); err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ? AND lease_until > CURRENT_TIMESTAMP",
		fence.TenantID, fence.KnowledgeID, fence.Generation, fence.ExecutionID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrProcessingFence
	}
	return nil
}

// PersistChunks commits ownership and SQL chunks together. Vector writes are external:
// callers must retain this ledger until all backends acknowledge exact-ID cleanup.
func (r *ProcessingFenceRepository) PersistChunks(ctx context.Context, fence ProcessingFence, chunks []*types.Chunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProcessingFence(tx, fence); err != nil {
			return err
		}
		for _, chunk := range chunks {
			if chunk == nil || chunk.ID == "" || chunk.TenantID != fence.TenantID || chunk.KnowledgeID != fence.KnowledgeID {
				return ErrProcessingFence
			}
			owner := processingOwnedChunk{TenantID: fence.TenantID, KnowledgeID: fence.KnowledgeID, Generation: fence.Generation, ExecutionID: fence.ExecutionID, ChunkID: chunk.ID}
			if err := tx.Create(&owner).Error; err != nil {
				return err
			}
			if err := tx.Create(chunk).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Finish does not Save a stale Knowledge snapshot. A stale/expired attempt cannot
// overwrite cancel, delete, a new generation, file metadata, or subtask counters.
func (r *ProcessingFenceRepository) Finish(ctx context.Context, fence ProcessingFence, status, code string) error {
	if status != types.ParseStatusFailed && status != types.ParseStatusFinalizing {
		return ErrProcessingFence
	}
	if code != "" && code != "index_failed" && code != "quota_exceeded" && code != "storage_read_failed" {
		return ErrProcessingFence
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProcessingFence(tx, fence); err != nil {
			return err
		}
		result := tx.Table("knowledges").Where("tenant_id = ? AND id = ? AND deleted_at IS NULL AND parse_status IN ?",
			fence.TenantID, fence.KnowledgeID, []string{types.ParseStatusPending, types.ParseStatusProcessing}).
			Updates(map[string]any{"parse_status": status, "error_message": code, "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProcessingFence
		}
		return nil
	})
}

// CleanupSQL targets only IDs owned by this capability, even after supersession.
// Returned IDs are the retryable vector-cleanup worklist, never a knowledge-wide delete.
func (r *ProcessingFenceRepository) CleanupSQL(ctx context.Context, fence ProcessingFence) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&processingOwnedChunk{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ? AND cleanup_complete = FALSE",
			fence.TenantID, fence.KnowledgeID, fence.Generation, fence.ExecutionID).Order("chunk_id").Pluck("chunk_id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Table("chunks").Where("tenant_id = ? AND knowledge_id = ? AND id IN ?", fence.TenantID, fence.KnowledgeID, ids).
			Updates(map[string]any{"deleted_at": gorm.Expr("CURRENT_TIMESTAMP"), "is_enabled": false}).Error
	})
	return ids, err
}

// CompleteCleanup is called only after every configured vector backend succeeds.
func (r *ProcessingFenceRepository) CompleteCleanup(ctx context.Context, fence ProcessingFence) error {
	return r.db.WithContext(ctx).Model(&processingOwnedChunk{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ?",
		fence.TenantID, fence.KnowledgeID, fence.Generation, fence.ExecutionID).Update("cleanup_complete", true).Error
}
