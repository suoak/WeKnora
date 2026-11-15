package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// PendingGeneralSyncInspector returns only original pending deliveries.
type PendingGeneralSyncInspector interface {
	PendingSharedSyncDeliveries(context.Context) ([]*asynq.TaskInfo, error)
}

// RecoverGeneralSyncCapacity preserves old deliveries while offering their
// exact first-attempt intent to the reserved queue. SQL generation claims make
// a later old delivery a harmless duplicate; no retry budget is restarted.
func RecoverGeneralSyncCapacity(
	ctx context.Context, db *gorm.DB, inspector PendingGeneralSyncInspector, queue interfaces.TaskEnqueuer,
) (int, error) {
	if inspector == nil || queue == nil {
		return 0, nil
	}
	tasks, err := inspector.PendingSharedSyncDeliveries(ctx)
	if err != nil {
		return 0, err
	}
	if len(tasks) > 100 {
		return 0, errors.New("shared sync delivery recovery exceeds bound")
	}
	count := 0
	for _, info := range tasks {
		if info == nil || info.Queue != types.QueueSync || info.Type != types.TypeDataSourceSync ||
			info.State != asynq.TaskStatePending || info.Retried != 0 || !info.LastFailedAt.IsZero() ||
			info.LastErr != "" || info.Group != "" || info.ID == "" {
			continue
		}
		var p types.DataSourceSyncPayload
		if json.Unmarshal(info.Payload, &p) != nil || p.TenantID == 0 || p.DataSourceID == "" || p.SyncLogID == "" {
			continue
		}
		var source types.DataSource
		if err := db.WithContext(ctx).Where("id=? AND tenant_id=? AND deleted_at IS NULL",
			p.DataSourceID, p.TenantID).Take(&source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return count, err
		}
		if types.FeishuConnectorType(source.Type) || source.Status != types.DataSourceStatusActive &&
			source.Status != types.DataSourceStatusError {
			continue
		}
		var valid int64
		if err := db.WithContext(ctx).Model(&types.SyncLog{}).
			Where("id=? AND data_source_id=? AND tenant_id=? AND status=?",
				p.SyncLogID, source.ID, source.TenantID, types.SyncLogStatusRunning).
			Where("execution_generation=0 AND NOT execution_claimed").Count(&valid).Error; err != nil {
			return count, err
		}
		if valid != 1 || !info.Deadline.IsZero() && !info.Deadline.After(time.Now()) {
			continue
		}
		opts := []asynq.Option{
			asynq.Queue(types.QueueGeneralSync), asynq.TaskID(info.ID), asynq.MaxRetry(info.MaxRetry),
			asynq.Retention(info.Retention),
		}
		if info.Timeout > 0 {
			opts = append(opts, asynq.Timeout(info.Timeout))
		}
		if !info.Deadline.IsZero() {
			opts = append(opts, asynq.Deadline(info.Deadline))
		}
		task := asynq.NewTaskWithHeaders(info.Type, info.Payload, info.Headers, opts...)
		if _, err := queue.Enqueue(task); err != nil {
			if errors.Is(err, asynq.ErrTaskIDConflict) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}
