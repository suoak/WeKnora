package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrProcessingFence is deliberately independent of driver error text.
var ErrProcessingFence = errors.New("processing execution is no longer current")

var ErrProcessingClaimBusy = errors.New("processing execution already claimed")

// ProcessingFence is an internal capability. It must never be exposed as an API credential.
type ProcessingFence struct {
	TenantID     uint64 `gorm:"primaryKey"`
	KnowledgeID  string `gorm:"primaryKey"`
	Generation   int64
	ExecutionID  string
	LeaseUntil   time.Time
	MainFinished bool
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

// CreateKnowledgeForProcessing commits a fresh upload's source, pending state
// and first generation together. Missing protocol tables fail the transaction;
// there is no fallback to an unprotected generation-zero enqueue.
func (r *knowledgeRepository) CreateKnowledgeForProcessing(ctx context.Context, knowledge *types.Knowledge) (int64, error) {
	tenant, ok := ctx.Value(types.TenantIDContextKey).(uint64)
	if !ok || tenant == 0 || knowledge == nil || knowledge.TenantID != tenant || knowledge.ID == "" || knowledge.KnowledgeBaseID == "" {
		return 0, ErrProcessingFence
	}
	copy := *knowledge
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb struct{ ID string }
		if err := tx.Table("knowledge_bases").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", copy.KnowledgeBaseID, tenant).Take(&kb).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProcessingFence
			}
			return err
		}
		copy.ParseStatus = types.ParseStatusPending
		copy.ErrorMessage = ""
		if err := tx.Create(&copy).Error; err != nil {
			return err
		}
		// Fresh IDs must not reuse a previously allocated capability.
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&now); err != nil {
			return err
		}
		return tx.Create(&ProcessingFence{TenantID: tenant, KnowledgeID: copy.ID, Generation: 1, LeaseUntil: now}).Error
	})
	if err != nil {
		return 0, err
	}
	*knowledge = copy
	return 1, nil
}

// Reserve allocates a user generation without granting a running lease. Queue
// latency must not consume an execution lease, and queue retry must not allocate
// another user generation. It uses the same knowledge-row lock as Allocate.
func (r *ProcessingFenceRepository) Reserve(ctx context.Context, tenantID uint64, knowledgeID string) (int64, error) {
	return r.reserve(ctx, tenantID, knowledgeID, false, nil)
}

// ReservePending orders a new queue admission and its pending state under the
// same row lock as worker writes. A failed transaction publishes neither state
// nor generation. Enqueue only the generation returned after this commit.
func (r *ProcessingFenceRepository) ReservePending(ctx context.Context, tenantID uint64, knowledgeID string) (int64, error) {
	return r.reserve(ctx, tenantID, knowledgeID, true, nil)
}

// ReservePendingWithMetadata publishes request-owned source metadata and the
// next queue generation in the same knowledge-row transaction. No worker state,
// tenant identity, counters, or execution fields may be supplied by a caller.
// This boundary is not activated in legacy admission until all writers migrate.
func (r *ProcessingFenceRepository) ReservePendingWithMetadata(ctx context.Context, tenantID uint64, knowledgeID string, metadata map[string]string) (int64, error) {
	copied := make(map[string]string, len(metadata))
	for field, value := range metadata {
		switch field {
		case "title", "file_path", "file_name", "file_type", "source", "embedding_model_id":
		case "metadata":
			if !json.Valid([]byte(value)) {
				return 0, ErrProcessingAdmissionMetadata
			}
		default:
			return 0, ErrProcessingAdmissionMetadata
		}
		copied[field] = value
	}
	return r.reserve(ctx, tenantID, knowledgeID, true, copied)
}

var ErrProcessingAdmissionMetadata = errors.New("processing admission metadata invalid")

func (r *ProcessingFenceRepository) reserve(ctx context.Context, tenantID uint64, knowledgeID string, pending bool, metadata map[string]string) (int64, error) {
	var generation int64
	if tenantID == 0 || knowledgeID == "" {
		return 0, ErrProcessingFence
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockProcessingKnowledge(tx, tenantID, knowledgeID); err != nil {
			return err
		}
		if pending {
			updates := map[string]any{"parse_status": types.ParseStatusPending, "error_message": "", "updated_at": gorm.Expr("CURRENT_TIMESTAMP")}
			for field, value := range metadata {
				updates[field] = value
			}
			result := tx.Table("knowledges").Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, knowledgeID).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrProcessingFence
			}
		}
		var current ProcessingFence
		err := tx.Where("tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID).Take(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		generation = current.Generation + 1
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&now); err != nil {
			return err
		}
		next := ProcessingFence{TenantID: tenantID, KnowledgeID: knowledgeID, Generation: generation, LeaseUntil: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "knowledge_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"generation", "execution_id", "lease_until", "main_finished"})}).Create(&next).Error
	})
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// FailPendingAdmission records a definite enqueue failure only while this exact
// generation is still unclaimed. An ambiguous queue timeout must never overwrite
// a worker that has already claimed the delivery, or a newer admission.
func (r *ProcessingFenceRepository) FailPendingAdmission(ctx context.Context, tenantID uint64, knowledgeID string, generation int64) error {
	if tenantID == 0 || knowledgeID == "" || generation <= 0 {
		return ErrProcessingFence
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockProcessingKnowledge(tx, tenantID, knowledgeID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProcessingFence
			}
			return err
		}
		var current ProcessingFence
		if err := tx.Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ? AND main_finished = FALSE", tenantID, knowledgeID, generation, "").Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProcessingFence
			}
			return err
		}
		result := tx.Table("knowledges").Where("tenant_id = ? AND id = ? AND deleted_at IS NULL AND parse_status = ?", tenantID, knowledgeID, types.ParseStatusPending).
			Updates(map[string]any{"parse_status": types.ParseStatusFailed, "error_message": "enqueue_failed", "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProcessingFence
		}
		return nil
	})
}

// Claim grants one execution nonce for a reserved generation. A concurrent
// redelivery returns Busy while the lease is live. Lease recovery changes nonce,
// fencing the crashed/partitioned worker without changing user generation.
func (r *ProcessingFenceRepository) Claim(ctx context.Context, tenantID uint64, knowledgeID string, generation int64, lease time.Duration) (ProcessingFence, error) {
	var claimed ProcessingFence
	if generation <= 0 || lease <= 0 || lease > time.Hour {
		return claimed, ErrProcessingFence
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockProcessingKnowledge(tx, tenantID, knowledgeID); err != nil {
			return err
		}
		var row struct{ ParseStatus string }
		if err := tx.Table("knowledges").Select("parse_status").Where("tenant_id = ? AND id = ?", tenantID, knowledgeID).Take(&row).Error; err != nil {
			return err
		}
		if row.ParseStatus != types.ParseStatusPending && row.ParseStatus != types.ParseStatusProcessing {
			return ErrProcessingFence
		}
		if err := tx.Where("tenant_id = ? AND knowledge_id = ? AND generation = ?", tenantID, knowledgeID, generation).Take(&claimed).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProcessingFence
			}
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&now); err != nil {
			return err
		}
		if claimed.MainFinished {
			return ErrProcessingFence
		}
		if claimed.ExecutionID != "" && claimed.LeaseUntil.After(now) {
			return ErrProcessingClaimBusy
		}
		claimed.ExecutionID = uuid.NewString()
		claimed.LeaseUntil = now.Add(lease)
		return tx.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ?", tenantID, knowledgeID, generation).
			Updates(map[string]any{"execution_id": claimed.ExecutionID, "lease_until": claimed.LeaseUntil}).Error
	})
	return claimed, err
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
			DoUpdates: clause.AssignmentColumns([]string{"generation", "execution_id", "lease_until", "main_finished"})}).Create(&next).Error
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

// Renew cannot revive an expired or superseded capability. The same row-lock
// protocol orders heartbeat, cancellation and generation allocation.
func (r *ProcessingFenceRepository) Renew(ctx context.Context, fence ProcessingFence, lease time.Duration) error {
	if lease <= 0 || lease > time.Hour {
		return ErrProcessingFence
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProcessingFence(tx, fence); err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&now); err != nil {
			return err
		}
		result := tx.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ?",
			fence.TenantID, fence.KnowledgeID, fence.Generation, fence.ExecutionID).Update("lease_until", now.Add(lease))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProcessingFence
		}
		return nil
	})
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

// Release closes the main worker lease without invalidating generation-only
// enrichment. A successful delivery cannot be reclaimed after lease expiry;
// a retryable failure can be reclaimed immediately with a different nonce.
func (r *ProcessingFenceRepository) Release(ctx context.Context, fence ProcessingFence, settled bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProcessingFence(tx, fence); err != nil {
			return err
		}
		values := map[string]any{"main_finished": settled, "lease_until": gorm.Expr("CURRENT_TIMESTAMP")}
		if err := tx.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id = ?", fence.TenantID, fence.KnowledgeID, fence.Generation, fence.ExecutionID).Updates(values).Error; err != nil {
			return err
		}
		if !settled {
			return tx.Table("knowledges").Where("tenant_id = ? AND id = ? AND parse_status = ? AND deleted_at IS NULL", fence.TenantID, fence.KnowledgeID, types.ParseStatusFailed).Update("parse_status", types.ParseStatusPending).Error
		}
		return nil
	})
}
