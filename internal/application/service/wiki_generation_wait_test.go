package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiGenerationWaitCoalescesBusyDeliveriesAndPreservesOwner(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	client := asynq.NewClientFromRedisClient(rc)
	svc := &wikiIngestService{redisClient: rc, task: client}
	inspector := asynq.NewInspectorFromRedisClient(rc)
	now := time.Unix(1_800_000_000, 0)
	ctx := context.Background()
	payload := WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "busy", Language: "zh-CN"}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	task := asynq.NewTask(wikiTaskType, data)
	require.NoError(t, wikilock.With(ctx, rc, "busy", func(context.Context) error {
		for range 50 {
			require.True(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now))
		}
		require.True(t, server.Exists("wiki:proof:generation:busy"))
		info, err := inspector.GetQueueInfo(types.QueueWiki)
		require.NoError(t, err)
		require.Equal(t, 1, info.Scheduled, "busy deliveries share one live successor")
		tasks, err := inspector.ListScheduledTasks(types.QueueWiki)
		require.NoError(t, err)
		require.Len(t, tasks, 1)
		require.JSONEq(t, string(data), string(tasks[0].Payload))
		require.Equal(t, 60*time.Minute, tasks[0].Timeout)
		// Once this delivery starts, its next contention gets a different ID.
		require.True(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now.Add(wikiGenerationWaitInterval)))
		info, err = inspector.GetQueueInfo(types.QueueWiki)
		require.NoError(t, err)
		require.Equal(t, 2, info.Scheduled)
		return nil
	}))
}

func TestWikiGenerationWaitDoesNotAcknowledgeTerminalOrWrongScope(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	svc := &wikiIngestService{redisClient: rc, task: asynq.NewClientFromRedisClient(rc)}
	inspector := asynq.NewInspectorFromRedisClient(rc)
	now := time.Unix(1_800_000_000, 0)
	ctx := context.Background()
	payload := WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "busy"}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	task := asynq.NewTask(wikiTaskType, data)
	require.True(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now))
	tasks, err := inspector.ListScheduledTasks(types.QueueWiki)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.NoError(t, inspector.ArchiveTask(types.QueueWiki, tasks[0].ID))
	require.False(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now), "an archive cannot replace a live delivery")
	payload.TenantID = 8
	require.False(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now), "another tenant cannot cover the original delivery")
	require.False(t, svc.coalesceWikiGenerationWait(ctx, task, WikiIngestPayload{}, now))
	finalize := asynq.NewTask(wikiFinalizeTaskType, data)
	require.True(t, svc.coalesceWikiGenerationWait(ctx, finalize, WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "busy"}, now))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.False(t, svc.coalesceWikiGenerationWait(canceled, task, payload, now))
	server.Close()
	require.False(t, svc.coalesceWikiGenerationWait(ctx, task, payload, now.Add(time.Minute)), "failed enqueue must preserve original retry")
}

func TestWikiGenerationWaitServiceAcknowledgesOnlyDurableSuccessor(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	svc := &wikiIngestService{
		redisClient: rc, task: asynq.NewClientFromRedisClient(rc),
		knowledgeSvc: &knowledgeService{config: &config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true}}},
	}
	data, err := json.Marshal(WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "busy"})
	require.NoError(t, err)
	task := asynq.NewTask(wikiTaskType, data)
	require.NoError(t, wikilock.With(context.Background(), rc, "busy", func(context.Context) error {
		callback := func(context.Context, *asynq.Task) error {
			t.Fatal("contender must not enter the current generation")
			return nil
		}
		require.NoError(t, svc.withGenerationLease(context.Background(), task, callback))
		inspector := asynq.NewInspectorFromRedisClient(rc)
		info, err := inspector.GetQueueInfo(types.QueueWiki)
		require.NoError(t, err)
		require.Equal(t, 1, info.Scheduled)
		svc.task = nil
		require.ErrorIs(t, svc.withGenerationLease(context.Background(), task, callback), ErrWikiIngestConcurrent)
		return nil
	}))
}

func TestScheduleFollowUpProofBackedGenerationStaysSingle(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	svc, rec := newFollowUpTestService(t, 1000)
	svc.redisClient = rc
	svc.knowledgeSvc = &knowledgeService{config: &config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true}}}
	require.True(t, svc.scheduleFollowUp(context.Background(), WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "busy"},
		wikiFollowUpDelay, 5, 32, false))
	require.Equal(t, 1, rec.count(), "one generation lease permits only one useful successor")
}
