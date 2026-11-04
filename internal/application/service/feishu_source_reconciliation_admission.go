package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
)

// The signed administrative recovery request is explicit publication approval
// for its one SQL-issued run. Reuse the established consent/binding checks;
// never create a second SyncLog that the controller's ticket cannot execute.
func (s *FeishuPersonalService) approveQueuedSourceReconciliation(
	ctx context.Context, ds *types.DataSource, run string,
) error {
	operator, ok := types.UserIDFromContext(ctx)
	if !ok || !types.IsSystemAdminFromContext(ctx) || types.PrivateKBReadUser(ctx) != operator || ds == nil {
		return access.ErrForbidden
	}
	policy := feishusynccontrol.ReconciliationPolicy(ctx, ds.ID)
	if policy == "" || run == "" {
		return access.ErrForbidden
	}
	ctx = types.WithLegacyKBAdminMetadata(ctx, ds.KnowledgeBaseID)
	_, err := s.approveSourceUpdates(ctx, ds.ID, operator, "signed-source-reconciliation:"+policy,
		time.Now().Add(15*time.Minute), run, true)
	return err
}
