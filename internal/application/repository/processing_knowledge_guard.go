package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// ProcessingFences exposes the same SQL connection to the orchestration adapter
// without broadening the historical public KnowledgeRepository contract.
func (r *knowledgeRepository) ProcessingFences() *ProcessingFenceRepository {
	return NewProcessingFenceRepository(r.db)
}

func (r *knowledgeRepository) processingWrite(ctx context.Context, knowledgeID string, operation func(*gorm.DB) error) error {
	execution, guarded := types.ProcessingExecutionFromContext(ctx)
	if !guarded {
		return operation(r.db.WithContext(ctx))
	}
	if execution.TenantID == 0 || execution.KnowledgeID != knowledgeID || execution.Generation <= 0 {
		return ErrProcessingFence
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockProcessingKnowledge(tx, execution.TenantID, knowledgeID); err != nil {
			return err
		}
		query := tx.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ? AND generation = ? AND execution_id <> ''",
			execution.TenantID, knowledgeID, execution.Generation)
		if execution.ExecutionID != "" {
			query = query.Where("execution_id = ? AND lease_until > CURRENT_TIMESTAMP", execution.ExecutionID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrProcessingFence
		}
		return operation(tx)
	})
}
