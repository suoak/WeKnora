package router

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// PendingSharedSyncDeliveries is bounded and preserves the original payload,
// headers, retry configuration and native task identity.
func (a *asynqTaskInspector) PendingSharedSyncDeliveries(ctx context.Context) ([]*asynq.TaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	queue, err := a.inspector.GetQueueInfo(types.QueueSync)
	if errors.Is(err, asynq.ErrQueueNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if queue.Paused {
		return nil, nil
	}
	pages := max(1, (queue.Pending+99)/100)
	page := int((a.generalSyncPage.Add(1)-1)%uint64(pages)) + 1
	return a.inspector.ListPendingTasks(types.QueueSync, asynq.PageSize(100), asynq.Page(page))
}
