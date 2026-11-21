package service

import (
	"context"
	"sort"

	"github.com/Tencent/WeKnora/internal/application/access"
)

// Start expensive targets early so a late large vector scan does not become
// the request's critical path. Keep original result slots and stable ties;
// neither scope, permission checks, ranking nor fan-out depends on the hints.
func portalSearchOrder(ctx context.Context, store access.DocumentPolicyStore, targets []*access.PortalKBTarget) []int {
	order := make([]int, len(targets))
	for i := range order {
		order[i] = i
	}
	loader, ok := store.(interface {
		PortalSearchWorkHints(context.Context, []*access.PortalKBTarget) []int64
	})
	if !ok {
		return order
	}
	hints := loader.PortalSearchWorkHints(ctx, targets)
	if len(hints) == len(order) {
		sort.SliceStable(order, func(i, j int) bool { return hints[order[i]] > hints[order[j]] })
	}
	return order
}
