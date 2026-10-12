package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type brokerAdmissionTenantFailure struct {
	interfaces.TenantRepository
	mu         sync.Mutex
	executions []types.ProcessingExecution
}

func (r *brokerAdmissionTenantFailure) GetTenantByID(ctx context.Context, _ uint64) (*types.Tenant, error) {
	capability, ok := types.ProcessingExecutionFromContext(ctx)
	if !ok {
		return nil, errors.New("broker_missing_execution")
	}
	r.mu.Lock()
	r.executions = append(r.executions, capability)
	r.mu.Unlock()
	return nil, errors.New("broker_controlled_dependency_failure")
}

func (r *brokerAdmissionTenantFailure) snapshot() []types.ProcessingExecution {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.ProcessingExecution(nil), r.executions...)
}

// The delivery is real Redis/Asynq. Only the producer's post-publish response
// boundary is fault-injected; this is not a simulated Redis transport timeout.
type brokerAdmissionLostReply struct{ client *asynq.Client }

func (q brokerAdmissionLostReply) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if _, err := q.client.Enqueue(task, opts...); err != nil {
		return nil, err
	}
	return nil, errors.New("broker_controlled_reply_loss")
}

func TestA3ProcessingExecutionPostgresRealBrokerAdmission(t *testing.T) {
	address := os.Getenv("KNOWHUB25_TEST_REDIS_ADDR")
	if address == "" {
		if os.Getenv("KNOWHUB25_REQUIRE_POSTGRES") == "1" {
			t.Fatal("required isolated Redis absent")
		}
		t.Skip("isolated Redis is not configured")
	}
	db, svc, fences := processingExecutionPG(t)
	require.NoError(t, db.Exec("DROP TABLE knowledges").Error)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb-1',1)").Error)
	svc.config = &config.Config{KnowledgeBase: &config.KnowledgeBaseConfig{FencedFileAdmission: true}}
	svc.kbService = &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 1}}
	svc.fileSvc = &createKnowledgeFileServiceStub{}
	tenantFailure := &brokerAdmissionTenantFailure{}
	svc.tenantRepo = tenantFailure
	options := asynq.RedisClientOpt{Addr: address, DB: 9}
	client := asynq.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	svc.task = client
	ctx := newCreateKnowledgeFileContext()
	// SQL commits and the real broker accepts the actual public Service payload.
	knowledge, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "broker.txt", "broker fixture"), nil, nil, "", nil, "", nil)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, knowledge.ParseStatus)
	inspector := asynq.NewInspector(options)
	t.Cleanup(func() { _ = inspector.Close() })
	queued, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, queued, 1)
	var payload types.DocumentProcessPayload
	require.NoError(t, json.Unmarshal(queued[0].Payload, &payload))
	require.Equal(t, knowledge.ID, payload.KnowledgeID)
	require.EqualValues(t, 1, payload.Generation)
	// Failure happens inside the actual ProcessDocument claim/lease boundary.
	// It deliberately does not assert that parsing or a Vector backend succeeded.
	delivered := make(chan error, 8)
	server := asynq.NewServer(options, asynq.Config{Concurrency: 1, Queues: map[string]int{"default": 1}, ShutdownTimeout: time.Second})
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDocumentProcess, func(work context.Context, task *asynq.Task) error {
		err := svc.ProcessDocument(work, task)
		delivered <- err
		if err != nil {
			return asynq.SkipRetry
		}
		return nil
	})
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)
	await := func() error {
		t.Helper()
		select {
		case err := <-delivered:
			return err
		case <-time.After(20 * time.Second):
			t.Fatal("broker_delivery_not_observed")
			return nil
		}
	}
	require.Error(t, await())
	first := tenantFailure.snapshot()
	require.Len(t, first, 1)
	require.NotEmpty(t, first[0].ExecutionID)
	require.EqualValues(t, 1, first[0].Generation)
	// A new real delivery of the same generation reclaims a different nonce.
	_, err = client.Enqueue(asynq.NewTask(types.TypeDocumentProcess, queued[0].Payload, asynq.MaxRetry(0)))
	require.NoError(t, err)
	require.Error(t, await())
	second := tenantFailure.snapshot()
	require.Len(t, second, 2)
	require.NotEqual(t, first[0].ExecutionID, second[1].ExecutionID)
	// A new generation makes a repeated old delivery harmless before body entry.
	generation, err := fences.ReservePending(ctx, 1, knowledge.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, generation)
	_, err = client.Enqueue(asynq.NewTask(types.TypeDocumentProcess, queued[0].Payload, asynq.MaxRetry(0)))
	require.NoError(t, err)
	require.NoError(t, await())
	require.Len(t, tenantFailure.snapshot(), 2)
	// Accepted publish + lost application reply cannot mark the committed row failed.
	svc.task = brokerAdmissionLostReply{client: client}
	uncertain, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "uncertain.txt", "uncertain fixture"), nil, nil, "", nil, "", nil)
	require.ErrorIs(t, err, errFileProcessingEnqueueUnknown)
	require.NotNil(t, uncertain)
	require.Error(t, await())
	stored, err := svc.repo.GetKnowledgeByID(ctx, 1, uncertain.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.Len(t, tenantFailure.snapshot(), 3)
	// A real closed producer rejects before sending. The public contract remains
	// conservative because TaskEnqueuer has no trusted definite-rejection type.
	closed := asynq.NewClient(options)
	require.NoError(t, closed.Close())
	svc.task = closed
	rejected, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "rejected.txt", "rejected fixture"), nil, nil, "", nil, "", nil)
	require.ErrorIs(t, err, errFileProcessingEnqueueUnknown)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, rejected.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	var fence repository.ProcessingFence
	require.NoError(t, db.Where("knowledge_id = ?", rejected.ID).Take(&fence).Error)
	require.Empty(t, fence.ExecutionID)
	require.False(t, fence.MainFinished)
}
