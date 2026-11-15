package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func wikiClaimPredicate(ctx context.Context, ids []int64) (string, []any, bool, error) {
	owner, present := types.WikiClaimOwnerFromContext(ctx)
	if !present {
		return "", nil, false, nil
	}
	if owner.TenantID == 0 || owner.KBID == "" || len(ids) > 10000 {
		return "", nil, true, types.ErrWikiClaimChanged
	}
	parts := make([]string, 0, len(ids))
	args := []any{owner.TenantID, owner.KBID, types.TaskScopeKnowledgeBase, types.TypeWikiIngest}
	for _, id := range ids {
		stamp, ok := owner.Claims[id]
		if !ok || stamp.IsZero() {
			return "", nil, true, types.ErrWikiClaimChanged
		}
		parts = append(parts, "(id=? AND claimed_at=?)")
		args = append(args, id, stamp)
	}
	predicate := "tenant_id=? AND scope_id=? AND scope=? AND task_type=? AND (" + strings.Join(parts, " OR ") + ")"
	return predicate, args, true, nil
}

func scopedWikiClaimWrite(ctx context.Context, query *gorm.DB, ids []int64) (*gorm.DB, bool, error) {
	predicate, args, scoped, err := wikiClaimPredicate(ctx, ids)
	if err != nil {
		return nil, scoped, err
	}
	if scoped {
		query = query.Where(predicate, args...)
	}
	return query, scoped, nil
}
