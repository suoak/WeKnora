package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecoverWikiClaimsForGeneration recovers prior-generation metadata only after
// the consumer owns the KB's existing exclusive publication/generation lease.
// No rows, payloads, retry counts, public content or source grants are removed.
func (r *taskPendingOpsRepository) RecoverWikiClaimsForGeneration(
	ctx context.Context, tenant uint64, kb string,
) (int64, error) {
	if tenant == 0 || kb == "" {
		return 0, types.ErrWikiClaimChanged
	}
	if !WikiDocumentSelectionForKB(kb) {
		return 0, nil
	}
	if err := wikilock.RequireOwned(ctx, kb); err != nil {
		return 0, err
	}
	var recovered int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor struct{ ID string }
		if err := tx.Table("knowledge_bases").Select("id").
			Where("id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Take(&anchor).Error; err != nil {
			return err
		}
		var rows []types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Select("id,claimed_at").Where(
			"tenant_id=? AND scope=? AND scope_id=? AND task_type=? AND claimed_at IS NOT NULL",
			tenant, types.TaskScopeKnowledgeBase, kb, types.TypeWikiIngest).
			Order("id").Limit(1000).Find(&rows).Error; err != nil {
			return err
		}
		// Check again after acquiring the SQL row locks. If Redis ownership
		// changed during the snapshot, none of that replacement's claims move.
		if err := wikilock.RequireOwned(ctx, kb); err != nil {
			return err
		}
		claims := make(map[int64]time.Time, len(rows))
		for _, row := range rows {
			if row.ClaimedAt == nil {
				continue
			}
			claims[row.ID] = *row.ClaimedAt
			result := tx.Model(&types.TaskPendingOp{}).Where(
				"id=? AND tenant_id=? AND scope=? AND scope_id=? AND task_type=? AND claimed_at=?",
				row.ID, tenant, types.TaskScopeKnowledgeBase, kb, types.TypeWikiIngest, *row.ClaimedAt,
			).UpdateColumn("claimed_at", nil)
			if result.Error != nil {
				return result.Error
			}
			recovered += result.RowsAffected
		}
		if recovered == 0 {
			return nil
		}
		payload, err := json.Marshal(map[string]any{"previous_claims": claims, "released": recovered})
		if err != nil {
			return err
		}
		return tx.Create(&types.TaskPendingOp{
			TenantID: tenant, TaskType: "wiki:claim_recovery", Scope: "wiki_claim_recovery_audit",
			ScopeID: kb, Op: "released", DedupKey: uuid.NewString(), Payload: payload, EnqueuedAt: time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return 0, err
	}
	return recovered, nil
}
