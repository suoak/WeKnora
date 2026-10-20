package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fileService "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newMVPFixture(t *testing.T) *personalRecoveryFixture {
	t.Helper()
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	f := newPersonalRecoveryFixture(t)
	f.ks.fileSvc = fileService.NewLocalFileService(t.TempDir(), "")
	f.ks.chunkRepo = repository.NewChunkRepository(f.db)
	return f
}
func mvpPublicTask(t *testing.T, f *personalRecoveryFixture, m *types.FeishuPersonalImport) (*types.Knowledge, *types.TaskPendingOp, types.DocumentProcessPayload) {
	t.Helper()
	var k types.Knowledge
	require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&k).Error)
	op, err := f.ks.repo.(interfaces.KnowledgeDispatchRepository).GetKnowledgeDispatch(f.ctx, k.TenantID, k.ID)
	require.NoError(t, err)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		require.Equal(t, 5, intent.PayloadVersion)
		require.Equal(t, types.TypeFeishuApplicationProcess, op.TaskType)
	} else {
		require.Equal(t, 3, intent.PayloadVersion)
	}
	require.Equal(t, k.ID, intent.Task.KnowledgeID)
	require.NotEqual(t, intent.Task.KnowledgeID, intent.Task.CandidateVersionID)
	return &k, op, intent.Task
}
func mvpAssertSingle(t *testing.T, f *personalRecoveryFixture, m *types.FeishuPersonalImport) {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
	k, _, _ := mvpPublicTask(t, f, m)
	file, err := f.ks.fileSvc.GetFile(f.ctx, k.FilePath)
	require.NoError(t, err, "the original stored file must remain readable")
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.NoError(t, file.Close())
	var mapped types.FeishuPersonalImport
	require.NoError(t, f.db.Where("id = ?", m.ID).First(&mapped).Error)
	require.Equal(t, k.ID, mapped.KnowledgeID)
}

func TestMVPFirstCreateIdempotencyPostgres(t *testing.T) {
	for _, scenario := range []string{"same_revision_and_changed_revision", "two_users_shared_resource", "concurrent_same_user", "partial_chunks_preserved"} {
		t.Run(scenario, func(t *testing.T) {
			f := newMVPFixture(t)
			m := f.prepare("https://tenant.feishu.cn/file/real")
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			k, _, task := mvpPublicTask(t, f, m)
			switch scenario {
			case "same_revision_and_changed_revision":
				again := f.prepare("https://tenant.feishu.cn/wiki/alias")
				require.Equal(t, m.LogicalKnowledgeID, again.LogicalKnowledgeID)
				require.Equal(t, m.AttemptID, again.AttemptID)
				require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
				f.revision.Store(2)
				_, err := f.s.Import(f.ctx, "kb", "https://tenant.feishu.cn/file/real")
				require.ErrorIs(t, err, repository.ErrPersonalUpdateDisabled)
			case "two_users_shared_resource":
				_, err := f.s.Import(f.userContext("B"), "kb", "https://tenant.feishu.cn/file/real")
				require.ErrorIs(t, err, repository.ErrPersonalAlreadyImported)
				var n int64
				require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Where("user_id = ?", "B").Count(&n).Error)
				require.Zero(t, n)
			case "concurrent_same_user":
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						got, err := f.s.Import(f.ctx, "kb", "https://tenant.feishu.cn/file/real")
						if err == nil && got.LogicalKnowledgeID != m.LogicalKnowledgeID {
							err = errors.New("KID changed")
						}
						errs <- err
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}
			case "partial_chunks_preserved":
				c := &types.Chunk{ID: "partial", TenantID: k.TenantID, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID, Content: "must survive unknown indexing", IsEnabled: true, ChunkType: types.ChunkTypeText}
				require.NoError(t, f.db.Create(c).Error)
				b, _ := json.Marshal(task)
				require.NoError(t, f.ks.ProcessDocument(f.ctx, asynq.NewTask(types.TypeDocumentProcess, b)))
				var saved types.Chunk
				require.NoError(t, f.db.Where("id = ?", c.ID).First(&saved).Error)
				require.Equal(t, c.Content, saved.Content)
				_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
				require.NoError(t, err)
				require.Equal(t, "needs_attention", a.Status)
			}
			mvpAssertSingle(t, f, m)
		})
	}
}

// Commit really reaches PG; the injected error loses only its acknowledgement.
type mvpBridgeFaultPool struct {
	*sql.DB
	mode *atomic.Int32
}
type mvpBridgeFaultTx struct {
	*sql.Tx
	mode *atomic.Int32
}

func (p mvpBridgeFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	return &mvpBridgeFaultTx{tx, p.mode}, err
}
func (t mvpBridgeFaultTx) Commit() error {
	var count int
	if err := t.Tx.QueryRow("SELECT count(*) FROM knowledges").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		switch t.mode.Swap(0) {
		case 1:
			if err := t.Tx.Commit(); err != nil {
				return err
			}
			return context.DeadlineExceeded
		case 2:
			_ = t.Tx.Rollback()
			return errors.New("injected rollback before public admission")
		}
	}
	return t.Tx.Commit()
}
func TestMVPFirstCreateCommitUnknownPostgres(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(map[int32]string{1: "commit_ack_lost", 2: "rollback_then_same_kid_retry"}[mode], func(t *testing.T) {
			f := newMVPFixture(t)
			m := f.prepare("https://tenant.feishu.cn/file/real")
			pool, err := f.db.DB()
			require.NoError(t, err)
			fault := &atomic.Int32{}
			fault.Store(mode)
			faultDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mvpBridgeFaultPool{pool, fault}}), &gorm.Config{})
			require.NoError(t, err)
			f.s.db = faultDB
			f.p.repo = repository.NewPersonalRecoveryRepository(faultDB)
			f.p.repo.FirstCreateOnly = true
			err = f.p.Process(f.ctx, f.payload(m.AttemptID))
			if mode == 1 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_after = clock_timestamp() - interval '1 second' WHERE id = ?", m.AttemptID).Error)
				require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			}
			mvpAssertSingle(t, f, m)
		})
	}
}

func TestMVPFirstCreateRedisRecoveryUsesOriginalKnowledge(t *testing.T) {
	addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
	if addr == "" {
		t.Skip("isolated Redis not configured")
	}
	require.Equal(t, "knowhub-p1-dispatch-redis-20261018:6379", addr)
	f := newMVPFixture(t)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	require.NoError(t, rdb.FlushDB(f.ctx).Err())
	down := asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
	defer down.Close()
	q := &dispatchActualQueue{client: down}
	f.ks.task = q
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, op, _ := mvpPublicTask(t, f, m)
	require.Equal(t, "pending", op.Op)
	// An interrupted, partially-persisted parse is deliberately attention.
	// This test proves queue delivery to the original KID, not final indexing.
	require.NoError(t, f.db.Create(&types.Chunk{ID: "partial", TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID, Content: "original partial", ChunkType: types.ChunkTypeText, IsEnabled: true}).Error)
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer client.Close()
	q.client = client
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
	defer inspector.Close()
	f.ks.taskInspector = dispatchActualInspector{raw: inspector}
	f.ks.redisClient = rdb
	restarted := restartKnowledgeServiceForTest(t, f.ks)
	require.NoError(t, restarted.RecoverKnowledgeDispatches(f.ctx))
	var received atomic.Bool
	server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 2, Queues: map[string]int{types.QueueDefault: 1}, TaskCheckInterval: 50 * time.Millisecond, ShutdownTimeout: time.Second})
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDocumentProcess, func(ctx context.Context, task *asynq.Task) error {
		var actual types.DocumentProcessPayload
		if err := json.Unmarshal(task.Payload(), &actual); err != nil {
			return err
		}
		if actual.KnowledgeID == k.ID {
			received.Store(true)
		}
		return restarted.ProcessDocument(ctx, task)
	})
	require.NoError(t, server.Start(mux))
	defer server.Shutdown()
	require.Eventually(t, func() bool { return received.Load() }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
		return err == nil && a.Status == "needs_attention"
	}, 10*time.Second, 50*time.Millisecond)
	mvpAssertSingle(t, f, m)
}
