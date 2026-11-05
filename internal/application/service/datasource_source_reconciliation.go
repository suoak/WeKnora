package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// RunSourceReconciliation uses the normal admission, durable SyncLog, queue,
// SQL execution Claim and atomic publication. Only a separately signed purpose
// admits quarantine reconciliation; ordinary manual/cron admission stays closed.
func (s *DataSourceService) RunSourceReconciliation(
	ctx context.Context, source, policy string,
) (*types.SyncLog, error) {
	m := feishusynccontrol.Recovery()
	if m == nil {
		return nil, feishusynccontrol.ErrRecoveryRejected
	}
	work, err := m.BeginSourceReconciliation(ctx, source, policy)
	if err != nil {
		return nil, fmt.Errorf("source_reconciliation_reservation: %w", err)
	}
	return s.manualSyncUsing(work, source, func(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		var payload types.DataSourceSyncPayload
		if task.Type() != types.TypeDataSourceSync || json.Unmarshal(task.Payload(), &payload) != nil ||
			payload.DataSourceID != source || payload.ReconciliationPolicyID != policy ||
			payload.Trigger != "source_reconciliation" || !payload.ForceFull {
			return nil, feishusynccontrol.ErrRecoveryRejected
		}
		if err := m.BindSourceReconciliationRun(work, source, payload.SyncLogID); err != nil {
			return nil, err
		}
		return s.taskEnqueuer.Enqueue(task, opts...)
	})
}

// The signed controller purpose and the existing personal-source SQL grant
// protect different boundaries. Prepare both for the SAME durable run instead
// of requiring an unrelated second operator action or creating a second queue.
func (s *DataSourceService) prepareSignedReferenceReconciliation(
	ctx context.Context, ds *types.DataSource,
) (*types.SyncLog, error) {
	if feishusynccontrol.ReconciliationPolicy(ctx, ds.ID) == "" || ds.LegacyAccessState != "hold" {
		return nil, nil
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return nil, err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil || ref == nil || ref.Kind != types.FeishuUserOAuth {
		return nil, err
	}
	manager := feishusynccontrol.Recovery()
	if manager == nil || s.feishuIdentity == nil {
		return nil, feishusynccontrol.ErrRecoveryRejected
	}
	expires, _, err := manager.ReconciliationAuthorization(ctx, ds.ID)
	if err != nil {
		return nil, fmt.Errorf("personal_reconciliation_authorization: %w", err)
	}
	operator := types.PrivateKBReadUser(ctx)
	if operator == "" {
		return nil, feishusynccontrol.ErrRecoveryRejected
	}
	var existing int64
	if err := s.feishuIdentity.db.WithContext(ctx).Model(&types.SystemSetting{}).
		Where("key=?", feishuSyncPolicyPrefix+ds.ID).Count(&existing).Error; err != nil {
		return nil, err
	}
	var grant *FeishuSyncPublicationPolicy
	reference := "signed-source-reconciliation:" + feishusynccontrol.ReconciliationPolicy(ctx, ds.ID)
	if existing == 0 {
		grant, err = s.feishuIdentity.ApproveSourceReconciliation(ctx, ds.ID, operator, reference, expires)
	} else {
		grant, err = s.feishuIdentity.RenewSourceReconciliation(ctx, ds.ID, operator, reference, expires)
	}
	if err != nil {
		return nil, fmt.Errorf("personal_reconciliation_preparation: %w", err)
	}
	return s.syncLogRepo.FindByID(ctx, grant.ReconciliationLogID)
}
