package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// EnterpriseReviewResource reads the existing policy and exact resource epoch.
// Missing policy is pending, never public. Callers must authorize metadata first.
func (r *EnterpriseAccessRepository) EnterpriseReviewResource(ctx context.Context, target types.EnterprisePolicyTarget) (types.EnterprisePolicy, string, uint64, error) {
	tx := r.db.WithContext(ctx)
	p, err := loadEnterprisePolicy(tx, target, false)
	if err != nil {
		return p, "", 0, err
	}
	v, tenant, err := resourceVersion(tx, target)
	return p, v, tenant, err
}
