package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

const wikiGenerationWaitInterval = 30 * time.Second

// A proof-backed KB has one generation lease. Preserve the original SQL work,
// but replace duplicate busy deliveries with a bounded, delayed wake-up. An
// enqueue failure or a terminal TaskID collision must never acknowledge work.
func (s *wikiIngestService) coalesceWikiGenerationWait(
	ctx context.Context, task *asynq.Task, payload WikiIngestPayload, now time.Time,
) bool {
	if ctx.Err() != nil || s.redisClient == nil || s.task == nil ||
		payload.TenantID == 0 || payload.KnowledgeBaseID == "" {
		return false
	}
	var original WikiIngestPayload
	if json.Unmarshal(task.Payload(), &original) != nil || original.TenantID != payload.TenantID ||
		original.KnowledgeBaseID != payload.KnowledgeBaseID {
		return false
	}
	timeout := 60 * time.Minute
	switch task.Type() {
	case wikiTaskType:
	case wikiFinalizeTaskType:
		timeout = 30 * time.Minute
	default:
		return false
	}
	// The next time bucket lets a contending successor replace itself. A
	// constant ID would collide with the caller's own active task forever.
	bucket := now.Unix()/int64(wikiGenerationWaitInterval/time.Second) + 1
	id := fmt.Sprintf("wiki-generation-wait:%d:%s:%s:%d",
		payload.TenantID, payload.KnowledgeBaseID, task.Type(), bucket)
	if current, _ := asynq.GetTaskID(ctx); current == id {
		return false
	}
	_, err := s.task.Enqueue(asynq.NewTask(task.Type(), task.Payload()),
		asynq.Queue(types.QueueWiki), asynq.TaskID(id),
		asynq.ProcessIn(wikiGenerationWaitInterval), asynq.Timeout(timeout),
		asynq.MaxRetry(wikiIngestMaxRetry))
	if err == nil {
		return true
	}
	if !errors.Is(err, asynq.ErrTaskIDConflict) && !errors.Is(err, asynq.ErrDuplicateTask) {
		return false
	}
	// TaskID conflicts also include archives and completed tasks. Only a live
	// delivery for this exact tenant, KB and task type can cover the caller.
	inspector := asynq.NewInspectorFromRedisClient(s.redisClient)
	info, err := inspector.GetTaskInfo(types.QueueWiki, id)
	if err != nil || info.Type != task.Type() {
		return false
	}
	var successor WikiIngestPayload
	if json.Unmarshal(info.Payload, &successor) != nil ||
		successor.TenantID != payload.TenantID || successor.KnowledgeBaseID != payload.KnowledgeBaseID {
		return false
	}
	switch info.State {
	case asynq.TaskStatePending, asynq.TaskStateScheduled, asynq.TaskStateRetry, asynq.TaskStateActive:
		return true
	default:
		return false
	}
}
