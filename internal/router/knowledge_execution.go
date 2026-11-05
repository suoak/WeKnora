package router

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// KnowledgeExecutionLeases requires both current queue membership and its
// server-maintained execution lease. A stale active record is not a live worker.
func (a *asynqTaskInspector) KnowledgeExecutionLeases(
	ctx context.Context, ids []string,
) (map[string]time.Time, error) {
	wanted, out := map[string]bool{}, map[string]time.Time{}
	for _, id := range ids {
		wanted[id] = true
	}
	if a.redis == nil || a.inspector == nil {
		return nil, fmt.Errorf("execution lease probe unavailable")
	}
	for _, queue := range queuesScanned {
		for page := 1; ; page++ {
			tasks, err := a.inspector.ListActiveTasks(queue, asynq.PageSize(listPageSize), asynq.Page(page))
			if isAsynqQueueNotFound(err) {
				break
			}
			if err != nil {
				return nil, err
			}
			for _, task := range tasks {
				var probe knowledgeIDProbe
				if json.Unmarshal(task.Payload, &probe) != nil || !wanted[probe.KnowledgeID] {
					continue
				}
				// Verify state and lease atomically, including the Redis clock.
				prefix := "asynq:{" + queue + "}:"
				lease, err := a.redis.Eval(ctx, `
local score=tonumber(redis.call('ZSCORE',KEYS[1],ARGV[1]) or '0')
local now=redis.call('TIME')
if redis.call('HGET',KEYS[2],'state')~='active' or score<=tonumber(now[1]) then return 0 end
return score`, []string{prefix + "lease", prefix + "t:" + task.ID}, task.ID).Int64()
				if err != nil {
					return nil, err
				}
				until := time.Unix(lease, 0)
				if lease > 0 && until.After(out[probe.KnowledgeID]) {
					out[probe.KnowledgeID] = until
				}
			}
			if len(tasks) < listPageSize {
				break
			}
		}
	}
	return out, nil
}
