package service

import (
	"context"
	"encoding/json"

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
		return nil, err
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
