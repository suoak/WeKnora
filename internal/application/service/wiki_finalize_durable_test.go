package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiFinalizeHistoricalContentionRecoveryKeepsBothArchives(t *testing.T) {
	for _, failure := range []error{ErrWikiIngestConcurrent, errors.New("real model failure")} {
		t.Run(failure.Error(), func(t *testing.T) {
			server := miniredis.RunT(t)
			rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = rc.Close() })
			queue := asynq.NewClientFromRedisClient(rc)
			inspector := asynq.NewInspectorFromRedisClient(rc)
			worker := asynq.NewServerFromRedisClient(rc, asynq.Config{
				Concurrency: 1, Queues: map[string]int{types.QueueWiki: 1},
				TaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
				LogLevel: asynq.FatalLevel,
			})
			require.NoError(t, worker.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
				return failure
			})))
			t.Cleanup(worker.Shutdown)
			db := syncdb.Open(t, "sqlite")
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
			pending := repository.NewTaskPendingOpsRepository(db)
			op := &types.TaskPendingOp{
				TenantID: 1, TaskType: wikiFinalizeTaskType, Scope: wikiTaskScope,
				ScopeID: "kb", Op: "slug", DedupKey: "concept/current",
			}
			require.NoError(t, pending.Enqueue(context.Background(), op))
			data := []byte(`{"tenant_id":1,"knowledge_base_id":"kb"}`)
			canonical := "wiki-finalize-kb"
			_, err := queue.Enqueue(asynq.NewTask(types.TypeWikiFinalize, data), asynq.Queue(types.QueueWiki),
				asynq.TaskID(canonical), asynq.ProcessIn(time.Hour))
			require.NoError(t, err)
			require.NoError(t, inspector.ArchiveTask(types.QueueWiki, canonical))
			recovery := fmt.Sprintf("%s-pending-%d", canonical, op.ID)
			_, err = queue.Enqueue(asynq.NewTask(types.TypeWikiFinalize, data), asynq.Queue(types.QueueWiki),
				asynq.TaskID(recovery), asynq.MaxRetry(0))
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				info, e := inspector.GetTaskInfo(types.QueueWiki, recovery)
				return e == nil && info.State == asynq.TaskStateArchived && info.LastErr == failure.Error()
			}, 5*time.Second, 10*time.Millisecond)
			worker.Stop()
			original, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
			require.NoError(t, err)
			before, err := inspector.GetTaskInfo(types.QueueWiki, recovery)
			require.NoError(t, err)
			svc := &wikiIngestService{task: queue, redisClient: rc, pendingRepo: pending}
			for range 5 {
				svc.scheduleFinalize(context.Background(), WikiIngestPayload{TenantID: 1, KnowledgeBaseID: "kb"})
			}
			scheduled, err := inspector.ListScheduledTasks(types.QueueWiki)
			require.NoError(t, err)
			if errors.Is(failure, ErrWikiIngestConcurrent) {
				require.Len(t, scheduled, 1)
				require.Equal(t, recovery+"-contention", scheduled[0].ID)
				require.Equal(t, wikiIngestMaxRetry, scheduled[0].MaxRetry)
			} else {
				require.Empty(t, scheduled, "real failures cannot create an endless replacement loop")
			}
			after, err := inspector.GetTaskInfo(types.QueueWiki, recovery)
			require.NoError(t, err)
			require.Equal(t, before, after)
			stillOriginal, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
			require.NoError(t, err)
			require.Equal(t, original, stillOriginal)
		})
	}
}

type durableFinalizeKB struct {
	interfaces.KnowledgeBaseService
	model string
}

func (s *durableFinalizeKB) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{
		ID: "kb", SummaryModelID: s.model,
		IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
	}, nil
}

type durableFinalizeModels struct {
	interfaces.ModelService
	model chat.Chat
	err   error
}

func (s *durableFinalizeModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.model, s.err
}

type durableFinalizePages struct {
	interfaces.WikiPageService
	updates int
}

func (s *durableFinalizePages) GetIndex(context.Context, string) (*types.WikiPage, error) {
	return &types.WikiPage{
		ID: "index", KnowledgeBaseID: "kb", Content: "existing verified introduction",
		LastEditSource: types.WikiEditSourcePipeline,
	}, nil
}

func (s *durableFinalizePages) UpdatePage(_ context.Context, p *types.WikiPage) (*types.WikiPage, error) {
	s.updates++
	return p, nil
}

func TestWikiFinalizeRetainsOrdinarySourceRowsOnGenerationFailure(t *testing.T) {
	for _, scenario := range []struct {
		name string
		id   string
		err  error
	}{
		{name: "no synthesis model"},
		{name: "model lookup failed", id: "model", err: errors.New("provider unavailable")},
		{name: "model call failed", id: "model"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			data, err := json.Marshal(wikiFinalizeRow{Change: &wikiFinalizeChange{
				Action:   wikiFinalizeAdded,
				DocTitle: "current local upload", DocSummary: "current verified text",
			}})
			require.NoError(t, err)
			row := &types.TaskPendingOp{ID: 7, Op: wikiFinalizeOpChange, Payload: data}
			pending := &folderPrunePendingRepoStub{rows: []*types.TaskPendingOp{row}}
			pages := &durableFinalizePages{}
			svc := &wikiIngestService{
				pendingRepo: pending, wikiService: pages, kbService: &durableFinalizeKB{model: scenario.id},
				modelService: &durableFinalizeModels{model: &mvpUnavailableIndexModel{}, err: scenario.err},
			}
			err = svc.ProcessWikiFinalize(context.Background(), asynq.NewTask(types.TypeWikiFinalize,
				[]byte(`{"tenant_id":1,"knowledge_base_id":"kb"}`)))
			require.Error(t, err)
			require.Empty(t, pending.deletedRowIDs, "non-Feishu source failures must retain the same durable rows")
			require.Equal(t, data, row.Payload)
			require.Zero(t, pages.updates, "a fallback cannot be published as successful generation")
		})
	}
}

func TestWikiFinalizeSchedulesSuccessorWhileCurrentCanonicalIDIsOccupied(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	queue := asynq.NewClientFromRedisClient(rc)
	inspector := asynq.NewInspectorFromRedisClient(rc)
	task, row := makeFolderPruneFinalizeTask(t)
	_, err := queue.Enqueue(task, asynq.Queue(types.QueueWiki), asynq.TaskID("wiki-finalize-kb-1"),
		asynq.ProcessIn(time.Hour))
	require.NoError(t, err)
	pending := &folderPrunePendingRepoStub{rows: []*types.TaskPendingOp{row}}
	svc := &wikiIngestService{
		pendingRepo: pending, wikiService: &folderPruneWikiServiceStub{},
		kbService: &folderPruneKBServiceStub{}, task: queue, redisClient: rc,
	}
	require.NoError(t, svc.ProcessWikiFinalize(context.Background(), task))
	scheduled, err := inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Len(t, scheduled, 2, "the current occupied ID cannot swallow the only next convergence pass")
	var successor bool
	for _, entry := range scheduled {
		successor = successor || entry.ID != "wiki-finalize-kb-1"
	}
	require.True(t, successor)
}
