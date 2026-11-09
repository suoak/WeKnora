package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestStartupWikiFinalizeRecoversOnlyContentionAndPreservesArchives(t *testing.T) {
	testStartupWikiFinalize(t, "")
}

func TestStartupWikiFinalizeRealRedis(t *testing.T) {
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	if addr == "" {
		t.Skip("isolated real Redis not configured")
	}
	testStartupWikiFinalize(t, addr)
}

func testStartupWikiFinalize(t *testing.T, addr string) {
	t.Helper()
	for _, failure := range []string{service.ErrWikiIngestConcurrent.Error(), "real model failure", "Feishu source access requires verification"} {
		t.Run(failure, func(t *testing.T) {
			db := setupResetPendingDB(t)
			kb := fmt.Sprintf("startup-%d", time.Now().UnixNano())
			require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES (?,7)", kb).Error)
			op := &types.TaskPendingOp{TenantID: 7, TaskType: types.TypeWikiFinalize,
				Scope: types.TaskScopeKnowledgeBase, ScopeID: kb, Op: "slug", DedupKey: "concept/test",
				Payload: types.JSON(`{}`)}
			require.NoError(t, db.Create(op).Error)
			address := addr
			if address == "" {
				address = miniredis.RunT(t).Addr()
			}
			rc := redis.NewClient(&redis.Options{Addr: address})
			t.Cleanup(func() { _ = rc.Close() })
			client := asynq.NewClientFromRedisClient(rc)
			inspector := asynq.NewInspectorFromRedisClient(rc)
			payload, err := json.Marshal(service.WikiIngestPayload{TenantID: 7, KnowledgeBaseID: kb})
			require.NoError(t, err)
			canonical := "wiki-finalize-" + kb
			_, err = client.Enqueue(asynq.NewTask(types.TypeWikiFinalize, payload),
				asynq.Queue(types.QueueWiki), asynq.TaskID(canonical), asynq.ProcessIn(time.Hour))
			require.NoError(t, err)
			require.NoError(t, inspector.ArchiveTask(types.QueueWiki, canonical))
			headID := fmt.Sprintf("%s-pending-%d", canonical, op.ID)
			worker := asynq.NewServerFromRedisClient(rc, asynq.Config{
				Concurrency: 1, Queues: map[string]int{types.QueueWiki: 1}, LogLevel: asynq.FatalLevel,
				TaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
			})
			require.NoError(t, worker.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
				return fmt.Errorf("%s", failure)
			})))
			t.Cleanup(worker.Shutdown)
			_, err = client.Enqueue(asynq.NewTask(types.TypeWikiFinalize, payload),
				asynq.Queue(types.QueueWiki), asynq.TaskID(headID), asynq.MaxRetry(0))
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				info, e := inspector.GetTaskInfo(types.QueueWiki, headID)
				return e == nil && info.State == asynq.TaskStateArchived && info.LastErr == failure
			}, 5*time.Second, 10*time.Millisecond)
			worker.Stop()
			before, err := inspector.GetTaskInfo(types.QueueWiki, headID)
			require.NoError(t, err)
			original, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
			require.NoError(t, err)
			for range 3 {
				recoverPendingWikiTasksWithRedis(db, client, rc)
			}
			scheduled, err := inspector.ListScheduledTasks(types.QueueWiki)
			require.NoError(t, err)
			var successors []*asynq.TaskInfo
			for _, info := range scheduled {
				if info.ID == headID+"-contention" {
					successors = append(successors, info)
				}
			}
			if failure == service.ErrWikiIngestConcurrent.Error() {
				require.Len(t, successors, 1)
			} else {
				require.Empty(t, successors)
			}
			after, err := inspector.GetTaskInfo(types.QueueWiki, headID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			stillOriginal, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
			require.NoError(t, err)
			require.Equal(t, original, stillOriginal)
			var retained types.TaskPendingOp
			require.NoError(t, db.First(&retained, op.ID).Error)
			require.Equal(t, op.Payload, retained.Payload)
			require.Nil(t, retained.ClaimedAt)
			require.Zero(t, retained.FailCount)
		})
	}
}
