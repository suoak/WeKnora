package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type personalRecoveryFixture struct {
	t         *testing.T
	db        *gorm.DB
	s         *FeishuPersonalService
	p         *PersonalRecovery
	ks        *knowledgeService
	ctx       context.Context
	revision  atomic.Int64
	downloads atomic.Int32
	block     func()
}

func newPersonalRecoveryFixture(t *testing.T) *personalRecoveryFixture {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost,open.feishu.cn")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db := syncdb.Open(t, "postgres")
	for _, ddl := range []string{
		"CREATE TABLE users(id varchar(36) PRIMARY KEY,is_active boolean,deleted_at timestamptz,is_system_admin boolean NOT NULL DEFAULT false)",
		"CREATE TABLE tenants(id bigint PRIMARY KEY,status text,deleted_at timestamptz)",
		"CREATE TABLE tenant_members(id bigserial PRIMARY KEY,user_id varchar(36),tenant_id bigint,role text,status text,deleted_at timestamptz)",
		"CREATE TABLE knowledge_bases(id varchar(36) PRIMARY KEY,tenant_id bigint,creator_id varchar(36),deleted_at timestamptz)",
		"INSERT INTO users(id,is_active,deleted_at) VALUES('A',true,NULL),('B',true,NULL)",
		"INSERT INTO tenants VALUES(1,'active',NULL)",
		"INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('A',1,'admin','active'),('B',1,'admin','active')",
		"INSERT INTO knowledge_bases VALUES('kb',1,'A',NULL)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	for _, name := range []string{"003025_feishu_personal", "003031_feishu_personal_recovery", "003033_feishu_application_grant"} {
		ddl, err := os.ReadFile("../../../migrations/versioned/" + name + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(ddl)).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.TaskPendingOp{}, &types.Chunk{}, &types.SystemSetting{}))
	settings := repository.NewSystemSettingRepository(db)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "A"}
	base := repository.NewKnowledgeRepository(db)
	ks := &knowledgeService{repo: base, task: &dispatchQueue{}, kbService: dispatchActualKB{kb: kb}}
	s := NewFeishuPersonalService(db, settings, ks, dispatchActualKB{kb: kb}, nil)
	admin := context.WithValue(personalUser("A"), types.SystemAdminContextKey, true)
	require.NoError(t, s.SaveApp(admin, &FeishuAppConfig{AppID: "cli_test", Secret: "synthetic-only", RedirectURI: "https://test.invalid/feishu/callback", Enabled: true}))
	for _, user := range []string{"A", "B"} {
		cipher, err := sealFeishu(feishuTokens{UserID: user, Access: "access-" + user, Refresh: "refresh-" + user})
		require.NoError(t, err)
		require.NoError(t, db.Create(&types.FeishuConnection{UserID: user, AppID: "cli_test", OpenID: "open-" + user, Tokens: cipher, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(2 * time.Hour)}).Error)
		require.NoError(t, db.Create(&types.FeishuPersonalGrantEpoch{UserID: user, Epoch: uuid.NewString()}).Error)
	}
	require.NoError(t, EnablePersonalRecovery(s, ks, nil, t.TempDir()))
	f := &personalRecoveryFixture{t: t, db: db, s: s, p: s.recovery, ks: ks}
	f.revision.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-A" && r.Header.Get("Authorization") != "Bearer access-B" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			io.WriteString(w, `{"code":0,"data":{"node":{"obj_type":"file","obj_token":"real"}}}`)
		case "/open-apis/drive/v1/metas/batch_query":
			fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real","title":"test.pdf","latest_modify_time":"%d"}]}}`, f.revision.Load())
		case "/open-apis/drive/v1/files/real/download":
			f.downloads.Add(1)
			if f.block != nil {
				f.block()
			}
			io.WriteString(w, "%PDF-1.7 synthetic candidate")
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	f.ctx = f.userContext("A")
	return f
}

func (f *personalRecoveryFixture) userContext(user string) context.Context {
	c := types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleAdmin}
	ctx := types.WithCaller(personalUser(user), c)
	g, err := access.ResolveKB(ctx, access.KBRequest{Caller: c}, &types.KnowledgeBase{ID: "kb", TenantID: 1}, types.OrgRoleEditor, nil, nil)
	require.NoError(f.t, err)
	return g.Context(ctx)
}

func (f *personalRecoveryFixture) prepare(url string) *types.FeishuPersonalImport {
	m, err := f.s.Import(f.ctx, "kb", url)
	require.NoError(f.t, err)
	require.NotEmpty(f.t, m.LogicalKnowledgeID)
	require.NotEmpty(f.t, m.AttemptID)
	return m
}

func (f *personalRecoveryFixture) payload(id string) types.DocumentProcessPayload {
	var op types.TaskPendingOp
	require.NoError(f.t, f.db.Where("dedup_key = ?", "personal-"+id).First(&op).Error)
	var envelope types.KnowledgeDispatch
	require.NoError(f.t, json.Unmarshal(op.Payload, &envelope))
	require.NotContains(f.t, string(op.Payload), "access-A")
	require.NotContains(f.t, string(op.Payload), "refresh-A")
	return envelope.Task
}

func (f *personalRecoveryFixture) check(m *types.FeishuPersonalImport, status string) {
	_, r, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(f.t, err)
	require.Equal(f.t, m.LogicalKnowledgeID, r.LogicalKnowledgeID)
	require.Equal(f.t, m.AttemptID, a.ID)
	require.Equal(f.t, status, a.Status)
	var n int64
	require.NoError(f.t, f.db.Model(&types.FeishuPersonalImport{}).Where("id = ?", m.ID).Count(&n).Error)
	require.EqualValues(f.t, 1, n)
	require.NoError(f.t, f.db.Model(&types.Knowledge{}).Where("id = ?", a.CandidateVersionID).Count(&n).Error)
	require.Zero(f.t, n, "candidate must never enter normal Knowledge")
	require.NoError(f.t, f.db.Model(&types.Chunk{}).Count(&n).Error)
	require.Zero(f.t, n, "P1-2 must not index private candidates")
	if status == "awaiting_index_isolation" {
		_, err = f.p.store.read(a)
		require.NoError(f.t, err)
	}
}

// Actual PG transaction wrapper: one failure is injected AFTER PostgreSQL has
// committed; another occurs before COMMIT and rolls the real transaction back.
type personalFaultPool struct {
	*sql.DB
	mode      *atomic.Int32
	onlyReady bool
}
type personalFaultTx struct {
	*sql.Tx
	mode      *atomic.Int32
	onlyReady bool
}

func (p personalFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	return &personalFaultTx{tx, p.mode, p.onlyReady}, err
}
func (t personalFaultTx) Commit() error {
	if t.onlyReady {
		var ready int
		if err := t.Tx.QueryRow("SELECT count(*) FROM feishu_personal_attempts WHERE status='awaiting_index_isolation'").Scan(&ready); err != nil {
			return err
		}
		if ready == 0 {
			return t.Tx.Commit()
		}
	}
	mode := t.mode.Swap(0)
	if mode == 2 {
		_ = t.Tx.Rollback()
		return errors.New("injected commit rollback")
	}
	err := t.Tx.Commit()
	if err == nil && mode == 1 {
		return errors.New("injected committed response timeout")
	}
	return err
}

func TestFeishuPersonalRecoveryPostgres(t *testing.T) {
	url := "https://team.feishu.cn/file/real"
	for _, name := range []string{"atomic_accept", "commit_unknown", "transaction_rollback", "legacy_knowledge_mapping_lost", "double_click", "wiki_direct_canonical", "two_users", "two_workers_claim", "late_old_worker", "unlink", "kb_permission_revoked", "app_config_changed", "new_revision_active_preserved", "accepted_before_work", "manifest_commit_unknown", "missing_dispatch_reconcile", "missing_candidate_file"} {
		t.Run(name, func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			if name == "commit_unknown" || name == "transaction_rollback" {
				pool, err := f.db.DB()
				require.NoError(t, err)
				mode := &atomic.Int32{}
				faultDB, err := gorm.Open(postgres.New(postgres.Config{Conn: personalFaultPool{pool, mode, false}}), &gorm.Config{})
				require.NoError(t, err)
				repo := repository.NewPersonalRecoveryRepository(faultDB)
				var epoch types.FeishuPersonalGrantEpoch
				require.NoError(t, f.db.Where("user_id = ?", "A").First(&epoch).Error)
				cfg, err := f.s.app(f.ctx)
				require.NoError(t, err)
				m := &types.FeishuPersonalImport{ID: uuid.NewString(), UserID: "A", AppID: "cli_test", OpenID: "open-A", KBID: "kb", ResourceType: "file", DocumentToken: "real", URL: url}
				a := &types.FeishuPersonalAttempt{CallerTenantID: 1, Revision: "1", AppConfigVersion: cfg.Version, GrantEpoch: epoch.Epoch}
				if name == "commit_unknown" {
					mode.Store(1)
				} else {
					mode.Store(2)
				}
				_, _, err = repo.Prepare(f.ctx, m, a)
				if name == "commit_unknown" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				var n int64
				require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&n).Error)
				if name == "transaction_rollback" {
					require.Zero(t, n)
				} else {
					require.EqualValues(t, 1, n)
				}
				first, second, err := repo.Prepare(f.ctx, m, a)
				require.NoError(t, err)
				again, last, err := repo.Prepare(f.ctx, m, a)
				require.NoError(t, err)
				require.Equal(t, first.LogicalKnowledgeID, again.LogicalKnowledgeID)
				require.Equal(t, second.ID, last.ID)
				require.Equal(t, second.CandidateVersionID, last.CandidateVersionID)
				m.LogicalKnowledgeID = first.LogicalKnowledgeID
				m.AttemptID = second.ID
				require.NoError(t, f.p.Process(f.ctx, f.payload(second.ID)))
				f.check(m, "awaiting_index_isolation")
				return
			}
			if name == "legacy_knowledge_mapping_lost" || name == "new_revision_active_preserved" {
				metadata, _ := json.Marshal(map[string]string{"feishu_imported_by": "A", "feishu_resource_id": "file:real", "feishu_open_id": "open-A"})
				require.NoError(t, f.db.Create(&types.Knowledge{ID: "original-K1", TenantID: 1, KnowledgeBaseID: "kb", Channel: "feishu_personal", Metadata: metadata, FilePath: "original-immutable-path", FileHash: "old-hash", ParseStatus: types.ParseStatusCompleted}).Error)
			}
			m := f.prepare(url)
			payload := f.payload(m.AttemptID)
			switch name {
			case "atomic_accept", "accepted_before_work":
				f.check(m, "pending")
			case "legacy_knowledge_mapping_lost":
				require.Equal(t, "original-K1", m.LogicalKnowledgeID)
			case "double_click", "wiki_direct_canonical":
				link := url
				if name == "wiki_direct_canonical" {
					link = "https://team.feishu.cn/wiki/node"
				}
				other := f.prepare(link)
				require.Equal(t, m.ID, other.ID)
				require.Equal(t, m.AttemptID, other.AttemptID)
				require.Equal(t, m.LogicalKnowledgeID, other.LogicalKnowledgeID)
			case "two_users":
				other, err := f.s.Import(f.userContext("B"), "kb", url)
				require.NoError(t, err)
				require.NotEqual(t, m.ID, other.ID)
				require.NotEqual(t, m.LogicalKnowledgeID, other.LogicalKnowledgeID)
				require.NotEqual(t, m.AttemptID, other.AttemptID)
				forged := payload
				forged.PersonalAttemptID = other.AttemptID
				require.Error(t, f.p.Process(f.ctx, forged))
			case "two_workers_claim":
				var success atomic.Int32
				var wg sync.WaitGroup
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if _, err := f.p.repo.Claim(f.ctx, m.AttemptID, 30*time.Second); err == nil {
							success.Add(1)
						}
					}()
				}
				wg.Wait()
				require.EqualValues(t, 1, success.Load())
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
			case "late_old_worker":
				old, err := f.p.repo.Claim(f.ctx, m.AttemptID, time.Second)
				require.NoError(t, err)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
				newOwner, err := f.p.repo.Claim(f.ctx, m.AttemptID, time.Second)
				require.NoError(t, err)
				require.Greater(t, newOwner.Fence, old.Fence)
				require.ErrorIs(t, f.p.repo.Mutate(f.ctx, old, 0, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
					a.ManifestHash = "late-old-writer"
					return nil
				}), repository.ErrPersonalFence)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
			case "unlink":
				require.NoError(t, f.s.Disconnect(personalUser("A")))
			case "kb_permission_revoked":
				require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='A'").Error)
			case "app_config_changed":
				cfg, err := f.s.app(f.ctx)
				require.NoError(t, err)
				require.NoError(t, f.s.SaveApp(context.WithValue(personalUser("A"), types.SystemAdminContextKey, true), cfg))
			case "new_revision_active_preserved":
				f.revision.Store(2)
				newM := f.prepare(url)
				require.Equal(t, m.ID, newM.ID)
				require.Equal(t, m.LogicalKnowledgeID, newM.LogicalKnowledgeID)
				require.NotEqual(t, m.AttemptID, newM.AttemptID)
				require.NoError(t, f.p.Process(f.ctx, payload))
				m = newM
				payload = f.payload(m.AttemptID)
			case "manifest_commit_unknown":
				owner, err := f.p.repo.Claim(f.ctx, m.AttemptID, time.Second)
				require.NoError(t, err)
				owner.ManifestKey = fmt.Sprintf("%s/%s/f%d", owner.ID, owner.CandidateVersionID, owner.Fence)
				require.NoError(t, f.p.repo.Mutate(f.ctx, owner, 0, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error { a.ManifestKey = owner.ManifestKey; return nil }))
				_, err = f.p.store.put(owner, []byte("%PDF-1.7 saved before DB confirmation"), "test.pdf")
				require.NoError(t, err)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
			case "missing_dispatch_reconcile":
				require.NoError(t, f.db.Where("dedup_key = ?", "personal-"+m.AttemptID).Delete(&types.TaskPendingOp{}).Error)
			}
			if name == "unlink" || name == "kb_permission_revoked" || name == "app_config_changed" {
				require.NoError(t, f.p.Reconcile(f.ctx))
				f.check(m, "needs_attention")
				require.Zero(t, f.downloads.Load())
				return
			}
			require.NoError(t, f.p.Reconcile(f.ctx))
			require.NoError(t, f.p.Process(f.ctx, payload))
			f.check(m, "awaiting_index_isolation")
			require.NoError(t, f.p.Process(f.ctx, payload))
			require.NoError(t, f.p.Reconcile(f.ctx))
			if name == "manifest_commit_unknown" {
				require.Zero(t, f.downloads.Load())
			} else {
				require.EqualValues(t, 1, f.downloads.Load())
			}
			if name == "missing_candidate_file" {
				_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
				require.NoError(t, err)
				path, err := f.p.store.path(a.ManifestKey)
				require.NoError(t, err)
				require.NoError(t, os.Remove(path+"/content"))
				require.NoError(t, f.p.Reconcile(f.ctx))
				f.check(m, "needs_attention")
			}
			if name == "new_revision_active_preserved" || name == "legacy_knowledge_mapping_lost" {
				var k types.Knowledge
				require.NoError(t, f.db.First(&k, "id = ?", "original-K1").Error)
				require.Equal(t, "original-immutable-path", k.FilePath)
				require.Equal(t, "old-hash", k.FileHash)
			}
		})
	}
}

var _ interfaces.TaskEnqueuer = (*dispatchQueue)(nil)

func TestFeishuPersonalRecoveryAsynqPostgres(t *testing.T) {
	addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("REQUIRE_SYNC_EXECUTION_POSTGRES") == "1" {
			t.Fatal("isolated Redis required")
		}
		t.Skip("isolated Redis not configured")
	}
	require.Equal(t, "knowhub-p1-dispatch-redis-20261018:6379", addr)
	for _, fault := range []string{"enqueue_failure", "enqueue_response_unknown", "redis_restart", "app_process_exit", "dispatch_ack_failure"} {
		t.Run(fault, func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			rdb := redis.NewClient(&redis.Options{Addr: addr})
			f.ks.redisClient = rdb
			defer rdb.Close()
			require.NoError(t, rdb.FlushDB(f.ctx).Err())
			client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
			defer client.Close()
			unavailable := asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
			defer unavailable.Close()
			q := &dispatchActualQueue{client: client, loseResponse: fault == "enqueue_response_unknown"}
			if fault == "enqueue_failure" {
				q.client = unavailable
			}
			f.ks.task = q
			inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
			defer inspector.Close()
			f.ks.taskInspector = dispatchActualInspector{raw: inspector}
			base := repository.NewKnowledgeRepository(f.db)
			wrapped := &dispatchFaultRepo{KnowledgeRepository: base, KnowledgeDispatchRepository: base.(interfaces.KnowledgeDispatchRepository), ackFailure: fault == "dispatch_ack_failure"}
			f.ks.repo = wrapped
			m := f.prepare("https://team.feishu.cn/file/real")
			_, _, original, err := f.p.repo.Load(f.ctx, m.AttemptID)
			require.NoError(t, err)
			if fault == "redis_restart" {
				_ = rdb.Shutdown(f.ctx).Err()
				require.Eventually(t, func() bool { return rdb.Ping(f.ctx).Err() == nil }, 20*time.Second, 100*time.Millisecond)
			}
			if fault == "app_process_exit" {
				dsn := f.db.Dialector.(*postgres.Dialector).Config.DSN
				cmd := exec.Command(os.Args[0], "-test.run=^TestFeishuPersonalRecoveryChild$")
				cmd.Env = append(os.Environ(), "FEISHU_PERSONAL_CHILD_DSN="+dsn, "FEISHU_PERSONAL_CHILD_ATTEMPT="+original.ID)
				err = cmd.Run()
				var exited *exec.ExitError
				require.ErrorAs(t, err, &exited)
				require.Equal(t, 19, exited.ExitCode())
				require.NoError(t, rdb.FlushDB(f.ctx).Err())
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", original.ID).Error)
			}
			q.client = client
			wrapped.ackFailure = false
			restarted := restartKnowledgeServiceForTest(t, f.ks)
			require.NoError(t, restarted.RecoverKnowledgeDispatches(f.ctx))
			require.NoError(t, restarted.RecoverKnowledgeDispatches(f.ctx))
			var executed atomic.Int32
			server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 2, Queues: map[string]int{types.QueueDefault: 1}, ShutdownTimeout: time.Second, TaskCheckInterval: 100 * time.Millisecond})
			mux := asynq.NewServeMux()
			mux.HandleFunc(types.TypeDocumentProcess, func(ctx context.Context, task *asynq.Task) error {
				executed.Add(1)
				return restarted.ProcessDocument(ctx, task)
			})
			require.NoError(t, server.Start(mux))
			defer server.Shutdown()
			require.Eventually(t, func() bool {
				_, _, a, e := f.p.repo.Load(f.ctx, original.ID)
				return e == nil && a.Status == "awaiting_index_isolation"
			}, 20*time.Second, 100*time.Millisecond)
			require.Positive(t, executed.Load())
			f.check(m, "awaiting_index_isolation")
			_, _, final, err := f.p.repo.Load(f.ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.CandidateVersionID, final.CandidateVersionID)
			require.Equal(t, original.LogicalKnowledgeID, final.LogicalKnowledgeID)
			require.NoError(t, restarted.RecoverKnowledgeDispatches(f.ctx))
			var count int64
			require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			var op types.TaskPendingOp
			require.NoError(t, f.db.Where("dedup_key = ?", "personal-"+original.ID).First(&op).Error)
			require.Equal(t, "completed", op.Op)
			require.EqualValues(t, 1, f.downloads.Load())
		})
	}
}

// A separate OS process claims the persisted attempt and exits abruptly.
func TestFeishuPersonalRecoveryChild(t *testing.T) {
	dsn := os.Getenv("FEISHU_PERSONAL_CHILD_DSN")
	if dsn == "" {
		return
	} // helper entry point; substantive case is the parent test.
	require.Contains(t, dsn, "sync_test_")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	_, err = repository.NewPersonalRecoveryRepository(db).Claim(context.Background(), os.Getenv("FEISHU_PERSONAL_CHILD_ATTEMPT"), 30*time.Second)
	require.NoError(t, err)
	os.Exit(19)
}

func TestFeishuPersonalRecoveryLateNetworkWorkerPostgres(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	m := f.prepare("https://team.feishu.cn/file/real")
	payload := f.payload(m.AttemptID)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f.block = func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	oldDone := make(chan error, 1)
	go func() { oldDone <- f.p.Process(context.Background(), payload) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("old worker did not enter remote request")
	}
	require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
	require.NoError(t, f.p.Process(context.Background(), payload))
	_, _, newer, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	close(release)
	require.Error(t, <-oldDone)
	_, _, final, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.Equal(t, newer.Fence, final.Fence)
	require.Equal(t, newer.ManifestKey, final.ManifestKey)
	require.Equal(t, newer.ManifestHash, final.ManifestHash)
	f.check(m, "awaiting_index_isolation")
}

func TestFeishuPersonalRecoveryConcurrentPreparePostgres(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	// Use the repository for concurrent acceptance, without an HTTP fixture or
	// queue mock deciding the identity. Real PG uniqueness/row locks settle it.
	var epoch types.FeishuPersonalGrantEpoch
	require.NoError(t, f.db.Where("user_id = ?", "A").First(&epoch).Error)
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	type result struct {
		r   *types.FeishuPersonalResource
		a   *types.FeishuPersonalAttempt
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			m := &types.FeishuPersonalImport{ID: uuid.NewString(), UserID: "A", AppID: "cli_test", OpenID: "open-A", KBID: "kb", ResourceType: "file", DocumentToken: "real", URL: "https://team.feishu.cn/file/real"}
			a := &types.FeishuPersonalAttempt{CallerTenantID: 1, Revision: "1", AppConfigVersion: cfg.Version, GrantEpoch: epoch.Epoch}
			r, accepted, err := f.p.repo.Prepare(context.Background(), m, a)
			results <- result{r, accepted, err}
		}()
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Equal(t, one.r.LogicalKnowledgeID, two.r.LogicalKnowledgeID)
	require.Equal(t, one.a.ID, two.a.ID)
	require.Equal(t, one.a.CandidateVersionID, two.a.CandidateVersionID)
	var count int64
	for _, model := range []any{&types.FeishuPersonalImport{}, &types.FeishuPersonalResource{}, &types.FeishuPersonalAttempt{}, &types.TaskPendingOp{}} {
		require.NoError(t, f.db.Model(model).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
}

func TestFeishuPersonalRecoveryCommitAuthorizationPostgres(t *testing.T) {
	for _, fault := range []string{"user_disabled", "member_revoked", "app_changed", "grant_rotated"} {
		t.Run(fault, func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			m := f.prepare("https://team.feishu.cn/file/real")
			owner, err := f.p.repo.Claim(f.ctx, m.AttemptID, 30*time.Second)
			require.NoError(t, err)
			switch fault {
			case "user_disabled":
				require.NoError(t, f.db.Exec("UPDATE users SET is_active=false WHERE id='A'").Error)
			case "member_revoked":
				require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='A'").Error)
			case "app_changed":
				cfg, err := f.s.app(f.ctx)
				require.NoError(t, err)
				require.NoError(t, f.s.SaveApp(context.WithValue(personalUser("A"), types.SystemAdminContextKey, true), cfg))
			case "grant_rotated":
				require.NoError(t, f.db.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").Update("epoch", uuid.NewString()).Error)
			}
			err = f.p.mutate(f.ctx, owner, 0, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
				a.ManifestHash = "must-not-commit"
				return nil
			})
			require.Error(t, err)
			_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
			require.NoError(t, err)
			require.Empty(t, a.ManifestHash)
			require.NoError(t, f.p.Reconcile(f.ctx))
			f.check(m, "needs_attention")
			require.Zero(t, f.downloads.Load())
		})
	}
}

func TestFeishuPersonalRecoveryGrantRefreshPostgres(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint("rejected=", reject), func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			m := f.prepare("https://team.feishu.cn/file/real")
			var before types.FeishuPersonalGrantEpoch
			require.NoError(t, f.db.First(&before, "user_id = ?", "A").Error)
			require.NoError(t, f.db.Model(&types.FeishuConnection{}).Where("user_id = ?", "A").Update("expires_at", time.Now().Add(-time.Second)).Error)
			f.s.api = &http.Client{Transport: feishuTestTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"access-A","refresh_token":"rotated-synthetic","expires_in":3600,"refresh_token_expires_in":7200}`
				status := 200
				if reject {
					body = `{"error":"invalid_grant"}`
					status = 400
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			err := f.p.Process(context.Background(), f.payload(m.AttemptID))
			if reject {
				require.Error(t, err)
				f.check(m, "needs_attention")
				require.Zero(t, f.downloads.Load())
			} else {
				require.NoError(t, err)
				f.check(m, "awaiting_index_isolation")
			}
			var after types.FeishuPersonalGrantEpoch
			require.NoError(t, f.db.First(&after, "user_id = ?", "A").Error)
			require.Equal(t, before.Epoch, after.Epoch, "refresh must preserve authorization epoch")
		})
	}
}

func TestFeishuPersonalRecoveryManifestConfirmationPostgres(t *testing.T) {
	for _, modeValue := range []int32{1, 2} {
		t.Run(fmt.Sprint("commit_mode=", modeValue), func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			m := f.prepare("https://team.feishu.cn/file/real")
			payload := f.payload(m.AttemptID)
			pool, err := f.db.DB()
			require.NoError(t, err)
			mode := &atomic.Int32{}
			mode.Store(modeValue)
			faultDB, err := gorm.Open(postgres.New(postgres.Config{Conn: personalFaultPool{pool, mode, true}}), &gorm.Config{})
			require.NoError(t, err)
			f.p.repo = repository.NewPersonalRecoveryRepository(faultDB)
			require.Error(t, f.p.Process(context.Background(), payload))
			_, _, saved, err := f.p.repo.Load(f.ctx, m.AttemptID)
			require.NoError(t, err)
			manifest, err := f.p.store.read(saved)
			require.NoError(t, err)
			if modeValue == 1 {
				require.Equal(t, "awaiting_index_isolation", saved.Status)
			} else {
				require.Equal(t, "recovery", saved.Status)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_after=clock_timestamp()-interval '1 second' WHERE id=?", saved.ID).Error)
			}
			require.NoError(t, f.p.Reconcile(context.Background()))
			require.NoError(t, f.p.Process(context.Background(), payload))
			_, _, final, err := f.p.repo.Load(f.ctx, m.AttemptID)
			require.NoError(t, err)
			require.Equal(t, saved.ManifestKey, final.ManifestKey)
			require.Equal(t, manifest.Hash, final.ManifestHash)
			require.Equal(t, saved.CandidateVersionID, final.CandidateVersionID)
			require.EqualValues(t, 1, f.downloads.Load())
			f.check(m, "awaiting_index_isolation")
		})
	}
}

func TestFeishuPersonalRecoveryTransientIdentityReadPostgres(t *testing.T) {
	for _, table := range []string{"feishu_personal_grant_epochs", "feishu_connections"} {
		t.Run(table, func(t *testing.T) {
			f := newPersonalRecoveryFixture(t)
			m := f.prepare("https://team.feishu.cn/file/real")
			var fired atomic.Bool
			name := "personal_identity_read_timeout"
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == table && fired.CompareAndSwap(false, true) {
					tx.AddError(context.DeadlineExceeded)
				}
			}))
			err := f.p.Reconcile(context.Background())
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.True(t, fired.Load())
			require.NoError(t, f.db.Callback().Query().Remove(name))
			f.check(m, "pending")
			require.NoError(t, f.p.Reconcile(context.Background()))
			require.NoError(t, f.p.Process(context.Background(), f.payload(m.AttemptID)))
			f.check(m, "awaiting_index_isolation")
		})
	}
}
