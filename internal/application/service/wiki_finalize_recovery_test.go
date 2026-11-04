package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiFinalizeArchivedTriggerRecoveryCoalescesAndPreservesHistory(t *testing.T) {
	mr := miniredis.RunT(t)
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: mr.Addr()})
	defer func() { require.NoError(t, client.Close()) }()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: mr.Addr()})
	defer func() { require.NoError(t, inspector.Close()) }()
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { require.NoError(t, rc.Close()) }()
	db := syncdb.Open(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	pending := repository.NewTaskPendingOpsRepository(db)
	ctx := context.Background()
	payload := WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "fixture-kb", Language: "zh-CN"}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	canonical := "wiki-finalize-" + payload.KnowledgeBaseID
	_, err = client.Enqueue(asynq.NewTask(types.TypeWikiFinalize, data,
		asynq.Queue(types.QueueWiki), asynq.TaskID(canonical), asynq.MaxRetry(0), asynq.ProcessIn(time.Hour)))
	require.NoError(t, err)
	svc := &wikiIngestService{task: client, redisClient: rc, pendingRepo: pending}
	op := &types.TaskPendingOp{
		TenantID: 1, TaskType: wikiFinalizeTaskType, Scope: wikiTaskScope,
		ScopeID: payload.KnowledgeBaseID, Op: "slug", DedupKey: "concept/fixture",
	}
	require.NoError(t, pending.Enqueue(ctx, op))
	svc.scheduleFinalize(ctx, payload)
	scheduled, err := inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Len(t, scheduled, 1, "a live canonical trigger must keep coalescing")
	require.NoError(t, inspector.ArchiveTask(types.QueueWiki, canonical))
	before, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
	require.NoError(t, err)
	for range 3 {
		svc.scheduleFinalize(ctx, payload)
	}
	scheduled, err = inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Len(t, scheduled, 1, "only one recovery trigger per durable head")
	require.Equal(t, fmt.Sprintf("%s-pending-%d", canonical, op.ID), scheduled[0].ID)
	require.Equal(t, wikiIngestMaxRetry, scheduled[0].MaxRetry)
	after, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
	require.NoError(t, err)
	require.Equal(t, before, after, "the original archive, identity and retry budget must be unchanged")
	require.NoError(t, inspector.ArchiveTask(types.QueueWiki, scheduled[0].ID))
	svc.scheduleFinalize(ctx, payload)
	scheduled, err = inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Empty(t, scheduled, "an unchanged failed head must not create another trigger")
	require.NoError(t, pending.DeleteByIDs(ctx, []int64{op.ID}))
	next := *op
	next.ID = 0
	require.NoError(t, pending.Enqueue(ctx, &next))
	svc.scheduleFinalize(ctx, payload)
	scheduled, err = inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Len(t, scheduled, 1, "actual durable progress permits the next convergence pass")
	require.Equal(t, fmt.Sprintf("%s-pending-%d", canonical, next.ID), scheduled[0].ID)
}
