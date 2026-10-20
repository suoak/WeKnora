package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dispatchActualQueue struct {
	client       *asynq.Client
	loseResponse bool
}

func (q *dispatchActualQueue) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	info, err := q.client.Enqueue(task, opts...)
	if err == nil && q.loseResponse {
		q.loseResponse = false
		return nil, errors.New("injected enqueue response timeout")
	}
	return info, err
}

type dispatchActualKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s dispatchActualKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}
func (s dispatchActualKB) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type dispatchActualFile struct{ createKnowledgeFileServiceStub }

func (s *dispatchActualFile) GetFile(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("# Recovery proof\n\nOriginal knowledge survives queue failure.")), nil
}

type dispatchActualTenant struct{ interfaces.TenantRepository }

func (dispatchActualTenant) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 1}, nil
}
func (dispatchActualTenant) AdjustStorageUsed(context.Context, uint64, int64) error { return nil }

// The adapter uses direct TaskID lookup, never a full Redis queue scan.
type dispatchActualInspector struct {
	interfaces.TaskInspector
	interfaces.RuntimeTaskInspector
	raw *asynq.Inspector
}

func (i dispatchActualInspector) GetRuntimeTask(_ context.Context, queue, id string) (*types.RuntimeTaskInfo, bool, error) {
	task, err := i.raw.GetTaskInfo(queue, id)
	if err != nil {
		return nil, true, err
	}
	var payload struct {
		KnowledgeID string `json:"knowledge_id"`
		TenantID    uint64 `json:"tenant_id"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, true, err
	}
	return &types.RuntimeTaskInfo{ID: id, Type: task.Type, KnowledgeID: payload.KnowledgeID, TenantID: payload.TenantID, State: types.RuntimeTaskState(task.State.String())}, true, nil
}
func (i dispatchActualInspector) RunRuntimeTask(_ context.Context, queue, id string) (bool, error) {
	return true, i.raw.RunTask(queue, id)
}

func TestFeishuDispatchAsynqPostgres(t *testing.T) {
	if mode := os.Getenv("FEISHU_DISPATCH_SUBPROCESS_MODE"); mode != "" {
		runDispatchSubprocess(t, mode)
		return
	}
	addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("REQUIRE_SYNC_EXECUTION_POSTGRES") == "1" {
			t.Fatal("real isolated Redis required")
		}
		t.Skip("isolated Redis not configured")
	}
	// Fault injection may flush and restart ONLY the explicitly named disposable
	// fixture, never an application's Redis selected from ambient configuration.
	require.Equal(t, "knowhub-p1-dispatch-redis-20261018:6379", addr)
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.TaskPendingOp{}, &types.Chunk{}))
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id varchar(64) PRIMARY KEY, tenant_id bigint NOT NULL, deleted_at timestamptz)").Error)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	unavailable := asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond})
	defer unavailable.Close()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
	defer inspector.Close()
	ctx := newCreateKnowledgeFileContext()
	for _, fault := range []string{"redis_unavailable", "enqueue_timeout_after_commit", "ack_write_failure", "lost_queue_before_worker", "redis_process_restart", "app_exit_after_accept", "worker_exit_before_chunks", "postprocess_queue_lost", "wiki_handoff_no_duplicate"} {
		t.Run(fault, func(t *testing.T) {
			require.NoError(t, rdb.FlushDB(ctx).Err())
			client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
			defer client.Close()
			for _, model := range []any{&types.TaskPendingOp{}, &types.Chunk{}, &types.Knowledge{}} {
				require.NoError(t, db.Unscoped().Where("1 = 1").Delete(model).Error)
			}
			base := repository.NewKnowledgeRepository(db)
			repo := &dispatchFaultRepo{KnowledgeRepository: base, KnowledgeDispatchRepository: base.(interfaces.KnowledgeDispatchRepository)}
			q := &dispatchActualQueue{client: client}
			if fault == "redis_unavailable" {
				q.client = unavailable
			}
			if fault == "enqueue_timeout_after_commit" {
				q.loseResponse = true
			}
			if fault == "ack_write_failure" {
				repo.ackFailure = true
			}
			fs := &dispatchActualFile{}
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Type: types.KnowledgeBaseTypeDocument, ChunkingConfig: types.ChunkingConfig{ChunkSize: 1024, ChunkOverlap: 32}}
			wantStatus := types.ParseStatusCompleted
			if fault == "wiki_handoff_no_duplicate" {
				kb.IndexingStrategy.WikiEnabled = true
				wantStatus = types.ParseStatusFinalizing
				require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)").Error)
			}
			svc := &knowledgeService{repo: repo, task: q, fileSvc: fs, kbService: dispatchActualKB{kb: kb}, tenantRepo: dispatchActualTenant{}, tenantService: &stubTenantServiceForModelDelete{}, chunkRepo: repository.NewChunkRepository(db), graphEngine: parentChildGraphRepo{}, taskInspector: dispatchActualInspector{raw: inspector}}
			k := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Channel: "feishu", FileName: "doc.md", FileType: "md", FileHash: "revision-1", FilePath: "stored/doc.md", ParseStatus: types.ParseStatusPending, EnableStatus: "disabled", Type: "file"}
			no := false
			require.NoError(t, k.SetProcessOverrides(&types.KnowledgeProcessOverrides{SummaryEnabled: &no}))
			var got *types.Knowledge
			var err error
			if fault == "app_exit_after_accept" {
				runDispatchChild(t, db, k, "accept")
				got, err = repo.GetKnowledgeByID(ctx, 1, k.ID)
			} else {
				got, err = svc.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
			}
			require.Equal(t, k.ID, got.ID)
			if fault == "redis_unavailable" || fault == "enqueue_timeout_after_commit" || fault == "ack_write_failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			op, err := repo.GetKnowledgeDispatch(ctx, 1, k.ID)
			require.NoError(t, err)
			require.Equal(t, k.ID, op.ScopeID)
			if fault == "lost_queue_before_worker" {
				require.NoError(t, rdb.FlushDB(ctx).Err())
			}
			if fault == "worker_exit_before_chunks" {
				runDispatchChild(t, db, k, "claim_exit")
			}
			if fault == "redis_process_restart" {
				_ = rdb.Shutdown(ctx).Err() // fixture has Docker restart=always, no persistence
				require.Eventually(t, func() bool { return rdb.Ping(ctx).Err() == nil }, 20*time.Second, 200*time.Millisecond)
			}
			q.client = client
			repo.ackFailure = false
			restarted := restartKnowledgeServiceForTest(t, svc)
			if fault == "app_exit_after_accept" {
				runDispatchChild(t, db, k, "recover")
			}
			require.NoError(t, restarted.RecoverKnowledgeDispatches(ctx))
			var processed atomic.Int64
			post := NewKnowledgePostProcessService(repo, svc.kbService, nil, svc.chunkRepo, q, repository.NewTaskPendingOpsRepository(db), nil, nil)
			queues := map[string]int{types.QueueDefault: 1, types.QueuePostProcess: 1}
			if fault == "postprocess_queue_lost" {
				delete(queues, types.QueuePostProcess)
			}
			server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 2, Queues: queues, ShutdownTimeout: time.Second, TaskCheckInterval: 100 * time.Millisecond})
			mux := asynq.NewServeMux()
			mux.HandleFunc(types.TypeDocumentProcess, func(worker context.Context, task *asynq.Task) error {
				processed.Add(1)
				return restarted.ProcessDocument(worker, task)
			})
			mux.HandleFunc(types.TypeKnowledgePostProcess, post.Handle)
			require.NoError(t, server.Start(mux))
			defer server.Shutdown()
			if fault == "postprocess_queue_lost" {
				require.Eventually(t, func() bool { op, e := repo.GetKnowledgeDispatch(ctx, 1, k.ID); return e == nil && op.Op == "processed" }, 20*time.Second, 100*time.Millisecond)
				require.NoError(t, rdb.FlushDB(ctx).Err())
				require.NoError(t, restarted.RecoverKnowledgeDispatches(ctx))
				postServer := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 2, Queues: map[string]int{types.QueuePostProcess: 1}, ShutdownTimeout: time.Second, TaskCheckInterval: 100 * time.Millisecond})
				require.NoError(t, postServer.Start(mux))
				defer postServer.Shutdown()
			}
			require.Eventually(t, func() bool {
				current, e := repo.GetKnowledgeByID(ctx, 1, k.ID)
				return e == nil && current.ParseStatus == wantStatus
			}, 20*time.Second, 100*time.Millisecond)
			require.Positive(t, processed.Load(), "real Asynq worker must execute the original ID")
			require.NoError(t, restarted.RecoverKnowledgeDispatches(ctx))
			op, err = repo.GetKnowledgeDispatch(ctx, 1, k.ID)
			require.NoError(t, err)
			require.Equal(t, "completed", op.Op)
			if fault == "wiki_handoff_no_duplicate" {
				payload, err := json.Marshal(types.KnowledgePostProcessPayload{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: k.ID, Attempt: 1})
				require.NoError(t, err)
				for i := 0; i < 2; i++ {
					require.NoError(t, post.Handle(ctx, asynq.NewTask(types.TypeKnowledgePostProcess, payload)))
				}
				var wikiOps int64
				require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("task_type = ? AND scope = ? AND dedup_key = ?", types.TypeWikiIngest, types.TaskScopeKnowledgeBase, k.ID).Count(&wikiOps).Error)
				require.EqualValues(t, 1, wikiOps, "duplicate dispatch must not create another Wiki operation")
			}
			var rows int64
			require.NoError(t, db.Model(&types.Knowledge{}).Count(&rows).Error)
			require.EqualValues(t, 1, rows)
			chunks, err := svc.chunkRepo.ListAllChunksByKnowledgeID(ctx, 1, k.ID)
			require.NoError(t, err)
			require.NotEmpty(t, chunks)
			require.Contains(t, chunks[0].Content, "Recovery proof")
			before := processed.Load()
			require.NoError(t, restarted.RecoverKnowledgeDispatches(ctx))
			time.Sleep(150 * time.Millisecond)
			require.Equal(t, before, processed.Load(), "completed intent must stop publishing")
			require.Zero(t, fs.deleteCalls)
		})
	}
}

// A genuinely separate process commits/recovers the intent and terminates;
// no in-memory object from that process can participate in the parent worker.
func runDispatchChild(t *testing.T, db *gorm.DB, k *types.Knowledge, mode string) {
	t.Helper()
	payload, err := json.Marshal(k)
	require.NoError(t, err)
	dsn := db.Dialector.(*postgres.Dialector).Config.DSN
	require.Contains(t, dsn, "sync_test_")
	cmd := exec.Command(os.Args[0], "-test.run=^TestFeishuDispatchAsynqPostgres$")
	cmd.Env = append(os.Environ(), "FEISHU_DISPATCH_SUBPROCESS_MODE="+mode, "FEISHU_DISPATCH_SUBPROCESS_DSN="+dsn, "FEISHU_DISPATCH_SUBPROCESS_KNOWLEDGE="+string(payload))
	err = cmd.Run()
	if mode == "claim_exit" {
		var exited *exec.ExitError
		require.ErrorAs(t, err, &exited)
		require.Equal(t, 17, exited.ExitCode())
	} else {
		require.NoError(t, err)
	}
}

func runDispatchSubprocess(t *testing.T, mode string) {
	dsn := os.Getenv("FEISHU_DISPATCH_SUBPROCESS_DSN")
	require.Contains(t, dsn, "sync_test_")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	var k types.Knowledge
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("FEISHU_DISPATCH_SUBPROCESS_KNOWLEDGE")), &k))
	base := repository.NewKnowledgeRepository(db)
	repo := base.(interfaces.KnowledgeDispatchRepository)
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")})
	defer client.Close()
	s := &knowledgeService{repo: base, task: client, kbService: dispatchActualKB{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, ChunkingConfig: types.ChunkingConfig{ChunkSize: 1024, ChunkOverlap: 32}}}}
	ctx := context.Background()
	switch mode {
	case "accept":
		op, err := s.dispatchIntent(ctx, &k)
		require.NoError(t, err)
		require.NoError(t, repo.AcceptKnowledgeDispatch(ctx, &k, op))
	case "recover":
		require.NoError(t, s.RecoverKnowledgeDispatches(ctx))
	case "claim_exit":
		require.NoError(t, repo.WithKnowledgeDispatchLock(ctx, k.ID, func(context.Context) error {
			op, err := repo.GetKnowledgeDispatch(ctx, 1, k.ID)
			require.NoError(t, err)
			require.NoError(t, repo.SetKnowledgeDispatchState(ctx, op.ID, "processing"))
			os.Exit(17)
			return nil
		}))
	default:
		t.Fatal("invalid helper mode")
	}
}
