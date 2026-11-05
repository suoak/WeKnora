package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// ReleaseWikiClaimsIfOwned releases only the exact leases returned by ClaimBatch.
// A canceled handler must never release a newer claim for the same durable row.
// Payloads, failure counts, original operation identities and receipts stay intact.
func (r *taskPendingOpsRepository) ReleaseWikiClaimsIfOwned(
	ctx context.Context, tenant uint64, kb string, claims map[int64]time.Time,
) (int64, error) {
	if tenant == 0 || kb == "" || len(claims) > 10000 {
		return 0, errors.New("invalid Wiki claim release scope")
	}
	if len(claims) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(claims))
	for id, stamp := range claims {
		if id <= 0 || stamp.IsZero() {
			return 0, errors.New("invalid Wiki claim release identity")
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var released int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			stamp := claims[id]
			result := tx.Model(&types.TaskPendingOp{}).
				Where("id=? AND tenant_id=? AND scope=? AND scope_id=? AND task_type=? AND claimed_at=?",
					id, tenant, types.TaskScopeKnowledgeBase, kb, types.TypeWikiIngest, stamp).
				UpdateColumn("claimed_at", nil)
			if result.Error != nil {
				return result.Error
			}
			released += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return released, nil
}
