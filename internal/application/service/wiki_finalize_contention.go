package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// RearmWikiFinalize uses the same bounded recovery as the ingest tail. Startup
// must not mistake an archived TaskID conflict for a live delivery. Original
// archives remain intact; only the existing contention-only successor is used.
func RearmWikiFinalize(ctx context.Context, task interfaces.TaskEnqueuer,
	pending interfaces.TaskPendingOpsRepository, rc *redis.Client, payload WikiIngestPayload,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := &wikiIngestService{task: task, pendingRepo: pending, redisClient: rc}
	return s.rearmFinalize(ctx, payload)
}

// Keep both archives intact. Only a historical lease-contention failure gets
// one replacement, still bounded by the durable head. Source/model failures
// remain closed and the worker revalidates every input before model I/O.
func (s *wikiIngestService) recoverContendedFinalize(
	ctx context.Context, task *asynq.Task, recoveryID string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	inspector := asynq.NewInspectorFromRedisClient(s.redisClient)
	info, err := inspector.GetTaskInfo(types.QueueWiki, recoveryID)
	if err != nil {
		return err
	}
	if info.State != asynq.TaskStateArchived || strings.TrimSpace(info.LastErr) != ErrWikiIngestConcurrent.Error() {
		return nil
	}
	_, err = s.task.Enqueue(task, asynq.TaskID(recoveryID+"-contention"))
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}
