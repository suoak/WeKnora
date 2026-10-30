package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// RunRecoveryCanary preserves normal admission/owner authorization and DB
// execution fencing. Only the delivery adapter changes: no task is inserted
// into or removed from the paused shared queue. The new SyncLog is durable.
func (s *DataSourceService) RunRecoveryCanary(
	ctx context.Context,
	source string,
	bind func(string, int64) error,
) error {
	var captured *asynq.Task
	log, err := s.manualSyncUsing(
		ctx,
		source,
		func(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
			var payload types.DataSourceSyncPayload
			if bind == nil ||
				captured != nil ||
				task.Type() != types.TypeDataSourceSync ||
				json.Unmarshal(task.Payload(), &payload) != nil ||
				payload.DataSourceID != source ||
				payload.ForceFull ||
				payload.Trigger != "manual" {
				return nil, feishusynccontrol.ErrRecoveryRejected
			}
			if err := bind(payload.SyncLogID, 1); err != nil {
				return nil, err
			}
			captured = task
			return &asynq.TaskInfo{ID: "recovery-" + payload.SyncLogID, Queue: "controlled-recovery"}, nil
		},
	)
	if err != nil {
		return err
	}
	if captured == nil || log == nil {
		return feishusynccontrol.ErrRecoveryRejected
	}
	if err = s.ProcessSync(ctx, captured); err != nil {
		return err
	}
	current, err := s.syncLogRepo.FindByID(ctx, log.ID)
	if err != nil {
		return err
	}
	if current == nil || current.Status != types.SyncLogStatusSuccess || current.ExecutionClaimed {
		return errors.New("recovery_canary_did_not_complete_successfully")
	}
	return nil
}
