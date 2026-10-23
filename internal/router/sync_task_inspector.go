package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// Archived/completed tasks are not live owners. Scan every live state and do
// not interpret a capped scan or Redis outage as evidence that work is absent.
func (a *asynqTaskInspector) LiveSyncTasks(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	// One atomic membership snapshot avoids missing a task moving between
	// pending/active/retry while separate state pages are scanned.
	if a.redis == nil {
		return nil, fmt.Errorf("sync queue Redis client unavailable")
	}
	prefix := "asynq:{" + types.QueueSync + "}:"
	ids, err := a.redis.Eval(ctx, `
local n=redis.call('LLEN',KEYS[1])+redis.call('LLEN',KEYS[2])+redis.call('ZCARD',KEYS[3])+redis.call('ZCARD',KEYS[4])
if n>100000 then return redis.error_reply('sync queue snapshot exceeds safety limit') end
local out={}
for i=1,4 do
 local v
 if i<3 then v=redis.call('LRANGE',KEYS[i],0,-1) else v=redis.call('ZRANGE',KEYS[i],0,-1) end
 for _,id in ipairs(v) do out[#out+1]=id end
end
return out`, []string{prefix + "pending", prefix + "active", prefix + "scheduled", prefix + "retry"}).StringSlice()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task, err := a.inspector.GetTaskInfo(types.QueueSync, id)
		if errors.Is(err, asynq.ErrTaskNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		{
			if task.Type != types.TypeDataSourceSync {
				continue
			}
			var p types.DataSourceSyncPayload
			if err := json.Unmarshal(task.Payload, &p); err != nil {
				return nil, fmt.Errorf("invalid live sync payload")
			}
			out[types.SyncTaskKey(p.TenantID, p.DataSourceID, p.SyncLogID)] = true
		}
	}
	return out, nil
}
