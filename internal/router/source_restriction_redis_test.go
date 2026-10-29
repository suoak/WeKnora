package router

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware/asynqdl"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type sourceRedisRepo struct{ interfaces.KnowledgeRepository }

func (sourceRedisRepo) KnowledgeSourceRestriction(
	_ context.Context,
	_ uint64,
	_, kid string,
) (types.KnowledgeSourceRestriction, error) {
	return types.KnowledgeSourceRestriction{Restricted: kid == "held", SourceState: "hold"}, nil
}

type sourceRedisAudit struct {
	interfaces.TaskDeadLetterRepository
	rows chan *types.TaskDeadLetter
}

func (r sourceRedisAudit) Insert(_ context.Context, row *types.TaskDeadLetter) error {
	r.rows <- row
	return nil
}

func TestSourceRestrictionRedisStopsWithoutRetryAndNormalTaskRuns(t *testing.T) {
	addr := os.Getenv("SOURCE_RESTRICTION_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires explicitly configured isolated Redis")
	}
	require.Contains(
		t,
		[]string{"redis:6379", "127.0.0.1:6379", "localhost:6379"},
		addr,
		"test runner must point at the explicit isolated Redis service",
	)
	queue := "source-restriction-test-" + uuid.NewString()
	opt := asynq.RedisClientOpt{Addr: addr, Password: os.Getenv("SOURCE_RESTRICTION_TEST_REDIS_PASSWORD")}
	inspector := asynq.NewInspector(opt)
	client := asynq.NewClient(opt)
	t.Cleanup(func() {
		require.True(t, strings.HasPrefix(queue, "source-restriction-test-"))
		_ = inspector.DeleteQueue(queue, true)
		_ = inspector.Close()
		_ = client.Close()
	})
	rows := make(chan *types.TaskDeadLetter, 2)
	normal := make(chan bool, 1)
	var bodyCalls, callbacks atomic.Int32
	mux := asynq.NewServeMux()
	mux.Use(
		asynqdl.MiddlewareWithCallback(
			sourceRedisAudit{rows: rows},
			func(context.Context, *asynq.Task, error) { callbacks.Add(1) },
		),
	)
	mux.Use(func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(service.WrapKnowledgeSourceAdmission(sourceRedisRepo{}, next.ProcessTask))
	})
	mux.HandleFunc(
		types.TypeDocumentProcess,
		func(context.Context, *asynq.Task) error { bodyCalls.Add(1); normal <- true; return nil },
	)
	server := asynq.NewServer(
		opt,
		asynq.Config{Concurrency: 1, Queues: map[string]int{queue: 1}, ShutdownTimeout: time.Second},
	)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)
	heldID := "held-" + uuid.NewString()
	_, err := client.Enqueue(
		asynq.NewTask(
			types.TypeDocumentProcess,
			[]byte(`{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"held","token":"sensitive-canary"}`),
		),
		asynq.Queue(queue),
		asynq.TaskID(heldID),
		asynq.MaxRetry(25),
	)
	require.NoError(t, err)
	select {
	case row := <-rows:
		require.Equal(t, "source_processing_restricted:hold", row.LastError)
		require.NotContains(t, string(row.Payload), "sensitive-canary")
	case <-time.After(8 * time.Second):
		t.Fatal("missing admission audit")
	}
	require.Eventually(t, func() bool {
		info, e := inspector.GetTaskInfo(queue, heldID)
		return e == nil && info.State == asynq.TaskStateArchived && info.Retried == 0
	}, 5*time.Second, 50*time.Millisecond)
	require.Zero(t, bodyCalls.Load())
	require.Zero(t, callbacks.Load())
	_, err = client.Enqueue(
		asynq.NewTask(
			types.TypeDocumentProcess,
			[]byte(`{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"normal"}`),
		),
		asynq.Queue(queue),
		asynq.MaxRetry(0),
	)
	require.NoError(t, err)
	select {
	case <-normal:
	case <-time.After(8 * time.Second):
		t.Fatal("normal task did not execute")
	}
	require.EqualValues(t, 1, bodyCalls.Load())
}
