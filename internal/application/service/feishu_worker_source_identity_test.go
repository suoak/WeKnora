package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
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

type workerSourceIdentityFixture struct {
	db     *gorm.DB
	svc    *DataSourceService
	source *types.DataSource
	user   *types.User
	remote atomic.Int32
	tokens atomic.Int32
	deny   atomic.Bool
	guard  *feishusafety.Guard
}

// Synthetic OAuth/Feishu wire only. PostgreSQL migrations, source permissions,
// generation claims, actual HTTP SSRF/safety transport and controller are real.
// No model requests, real remote ACL claims or successful publication receipts.
func newWorkerSourceIdentityFixture(t *testing.T, kinds ...string) *workerSourceIdentityFixture {
	t.Helper()
	kind := types.FeishuUserOAuth
	if len(kinds) > 0 {
		require.Len(t, kinds, 1)
		kind = kinds[0]
	}
	executable := os.Getenv("FEISHU_SAFETY_REDIS_EXECUTABLE")
	if os.Getenv("REQUIRE_PRIVATE_POSTGRES") == "1" {
		require.NotEmpty(t, executable, "required native isolation must never become a skipped Redis fixture")
		require.NotEmpty(t, os.Getenv("FEISHU_LEGACY_TEST_DSN"), "required disposable SQL fixture is missing")
	}
	if executable == "" {
		t.Skip("requires explicitly owned Redis executable and disposable PostgreSQL")
	}
	dsn := feishulegacy.PrivateDSN(t)
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	t.Setenv("FEISHU_APPLICATION_GRANT_ENABLED", "0")
	t.Setenv("FEISHU_APPLICATION_GRANT_TEST_ONLY", "0")
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	if kind == types.FeishuApplicationGrant {
		t.Setenv("FEISHU_APPLICATION_GRANT_ENABLED", "1")
		require.True(
			t,
			applicationGrantEngineAvailable(),
			"application grant requires the actual native AnyDoc build",
		)
	}
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	db = types.EnablePrivateKBGuards(db)
	f := &workerSourceIdentityFixture{db: db}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.remote.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			if kind != types.FeishuApplicationGrant {
				http.Error(w, "user OAuth must not fall back to app tokens", http.StatusForbidden)
				return
			}
			var credentials struct {
				AppID     string `json:"app_id"`
				AppSecret string `json:"app_secret"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&credentials) != nil ||
				credentials.AppID != "cli_worker_fixture" || credentials.AppSecret != "fixture-only" {
				http.Error(w, "fixture application identity rejected", http.StatusForbidden)
				return
			}
			f.tokens.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"fixture-app-token","expire":7200}`)
			return
		}
		bearer, resourceType := "fixture-user-token", "file"
		if kind == types.FeishuApplicationGrant {
			bearer, resourceType = "fixture-app-token", "docx"
		}
		if r.Header.Get("Authorization") != "Bearer "+bearer {
			http.Error(w, "fixture identity rejected", http.StatusForbidden)
			return
		}
		if f.deny.Load() {
			_, _ = io.WriteString(w, `{"code":131006,"msg":"fixture document permission denied"}`)
			return
		}
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"node":{"node_token":"fixturewiki",`+
				`"obj_type":"%s","obj_token":"fixturefile","title":"fixture","obj_edit_time":"1792800000"}}}`,
				resourceType)
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"%s",`+
				`"doc_token":"fixturefile","title":"fixture","latest_modify_time":"1792800000"}]}}`, resourceType)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remote.Close)
	tenant := &types.Tenant{ID: 810270, Name: "worker-identity-fixture", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	f.user = &types.User{
		ID: uuid.NewString(), Username: "fixture-owner", Email: "owner@example.invalid",
		PasswordHash: "no-real-login", TenantID: tenant.ID, IsActive: true,
	}
	require.NoError(t, db.Create(f.user).Error)
	require.NoError(t, db.Create(&types.TenantMember{
		TenantID: tenant.ID, UserID: f.user.ID,
		Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive,
	}).Error)
	kb := &types.KnowledgeBase{
		ID: uuid.NewString(), TenantID: tenant.ID,
		CreatorID: f.user.ID, Name: "worker-identity", Type: types.KnowledgeBaseTypeDocument,
	}
	require.NoError(t, db.Create(kb).Error)
	if kind == types.FeishuUserOAuth {
		require.NoError(
			t,
			db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES(?,?)", kb.ID, f.user.ID).Error,
		)
	}
	kbs := &legacyLiveKB{db: db}
	ks := &knowledgeService{kbService: kbs, repo: repository.NewKnowledgeRepository(db)}
	identity := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, kbs, nil)
	require.NoError(t, EnablePersonalRecovery(identity, ks, nil, t.TempDir()))
	identity.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = remote.URL; return core.NewClient(cfg) }
	ownerCtx := types.WithCaller(context.Background(), types.Caller{
		TenantID: tenant.ID,
		UserID:   f.user.ID, Role: types.TenantRoleContributor,
	})
	ownerCtx = context.WithValue(ownerCtx, types.UserIDContextKey, f.user.ID)
	ownerCtx = types.WithPrincipal(ownerCtx, types.Principal{Type: types.PrincipalWebUser, ID: f.user.ID})
	ownerCtx = context.WithValue(ownerCtx, types.SystemAdminContextKey, false)
	admin := &types.User{
		ID:            uuid.NewString(),
		Username:      "fixture-config-admin",
		Email:         "admin@example.invalid",
		PasswordHash:  "no-real-login",
		TenantID:      tenant.ID,
		IsActive:      true,
		IsSystemAdmin: true,
	}
	require.NoError(t, db.Create(admin).Error)
	require.NoError(t, db.Create(&types.TenantMember{
		TenantID: tenant.ID, UserID: admin.ID,
		Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive,
	}).Error)
	adminCtx := types.WithCaller(context.Background(), types.Caller{
		TenantID: tenant.ID,
		UserID:   admin.ID, Role: types.TenantRoleAdmin,
	})
	adminCtx = context.WithValue(adminCtx, types.UserIDContextKey, admin.ID)
	adminCtx = types.WithPrincipal(adminCtx, types.Principal{Type: types.PrincipalWebUser, ID: admin.ID})
	adminCtx = context.WithValue(adminCtx, types.SystemAdminContextKey, admin.IsSystemAdmin)
	require.NoError(t, identity.SaveApp(adminCtx, &FeishuAppConfig{
		AppID:  "cli_worker_fixture",
		Secret: "fixture-only", Enabled: true, RedirectURI: "https://fixture.invalid/feishu/callback",
		Capabilities: &types.FeishuAccessCapabilities{
			PersonalEnabled: true, ApplicationEnabled: kind == types.FeishuApplicationGrant,
		},
	}))
	cipher, err := sealFeishu(
		feishuTokens{UserID: f.user.ID, Access: "fixture-user-token", Refresh: "fixture-refresh"},
	)
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.FeishuConnection{
		UserID: f.user.ID, AppID: "cli_worker_fixture",
		OpenID: "fixture-open", Tokens: cipher, ExpiresAt: time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(2 * time.Hour),
	}).Error)
	require.NoError(
		t,
		db.Create(&types.FeishuPersonalGrantEpoch{UserID: f.user.ID, Epoch: uuid.NewString()}).Error,
	)
	_, _, credential, err := identity.ResolveCredentialReference(ownerCtx, kind, "")
	require.NoError(t, err)
	cfg := &types.DataSourceConfig{ResourceIDs: []string{"links"}, Settings: map[string]interface{}{
		"links": []string{"https://fixture.feishu.cn/wiki/fixturewiki"},
		"credential_reference": types.FeishuSourceReference{
			Version: 1, OwnerUserID: f.user.ID,
			Kind: credential.Kind, ConnectionID: credential.ConnectionID, AppID: credential.AppID,
			IdentityVersion: credential.ConfigVersion,
		},
	}}
	blob, err := cfg.ToJSON()
	require.NoError(t, err)
	f.source = &types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Name: "worker-identity", Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive, Config: blob,
	}
	require.NoError(t, db.Create(f.source).Error)
	connector := &legacySyncFixture{kind: types.ConnectorTypeFeishu}
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(connector))
	f.svc = &DataSourceService{
		dsRepo:      repository.NewDataSourceRepository(db),
		syncLogRepo: repository.NewSyncLogRepository(db), kbService: kbs, feishuIdentity: identity,
		knowledgeService:  ks,
		connectorRegistry: registry, tenantRepo: repository.NewTenantRepository(db), tagService: &legacyNoAutoTag{},
	}
	f.startAutomaticWorker(t, executable)
	return f
}

func (f *workerSourceIdentityFixture) startAutomaticWorker(t *testing.T, executable string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "kh-worker-redis-")
	require.NoError(t, err)
	socket := filepath.Join(dir, "redis.sock")
	command := exec.Command(executable, "--port", "0", "--unixsocket", socket,
		"--unixsocketperm", "700", "--save", "", "--appendonly", "no")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	require.NoError(t, command.Start())
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	r := redis.NewClient(&redis.Options{
		Network: "unix", Addr: socket, MaxRetries: -1,
		ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = r.ShutdownNoSave(context.Background()).Err()
		_ = r.Close()
		select {
		case <-processDone:
		case <-time.After(time.Second):
			_ = command.Process.Kill()
			<-processDone
		}
		require.NoError(t, os.RemoveAll(dir)) // Exact generated test-owned directory.
	})
	require.Eventually(t, func() bool { return r.Ping(context.Background()).Err() == nil },
		3*time.Second, 20*time.Millisecond)
	server := asynq.NewServer(asynq.RedisClientOpt{Network: "unix", Addr: socket}, asynq.Config{
		Concurrency: 1, Queues: map[string]int{"sync": 1, "approved_sync": 1}, ShutdownTimeout: time.Second,
	})
	require.NoError(
		t,
		server.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })),
	)
	t.Cleanup(server.Shutdown)
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "worker-identity-" + uuid.NewString()
	f.guard, err = feishusafety.New(r, policy)
	require.NoError(t, err)
	host, err := os.Hostname()
	require.NoError(t, err)
	c, err := feishusynccontrol.New(
		r,
		f.guard,
		feishusynccontrol.Config{
			Automatic:        true,
			AutomaticService: true, DedicatedSyncLane: true, Scope: "all", Maintenance: 4,
			SourceSHA: strings.Repeat("a", 40), WorkerID: fmt.Sprintf("%s:%d", host, os.Getpid()),
		},
		func(ctx context.Context) error { _, err := repository.FenceLegacySyncExecutions(ctx, f.db); return err },
		func(feishusynccontrol.Event) error { return nil },
	)
	require.NoError(t, err)
	c.ApprovedConsumers = feishusynccontrol.ApprovedConsumers(r)
	m, err := feishusynccontrol.NewAutomaticServiceManager(c, f.db,
		repository.NewTenantAPIKeyRepository(f.db).(feishusynccontrol.AutomaticSourceKeyScopes))
	require.NoError(t, err)
	oldGuard, oldRecovery := feishusafety.Default(), feishusynccontrol.Recovery()
	feishusafety.InstallDefault(f.guard)
	feishusynccontrol.InstallRecovery(m)
	stop, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	running := false
	t.Cleanup(func() {
		cancel()
		if running {
			<-done
		}
		feishusafety.InstallDefault(oldGuard)
		feishusynccontrol.InstallRecovery(oldRecovery)
	})
	require.Eventually(t, func() bool {
		ctx, end := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer end()
		return c.Step(ctx) == nil
	}, 8*time.Second, 150*time.Millisecond)
	running = true
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop.Done():
				return
			case <-ticker.C:
				ctx, end := context.WithTimeout(stop, 500*time.Millisecond)
				_ = c.Step(ctx) // Any failed qualification remains fail-closed.
				end()
			}
		}
	}()
}

func (f *workerSourceIdentityFixture) claim(t *testing.T) context.Context {
	t.Helper()
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: f.source.ID,
		TenantID: f.source.TenantID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	created, err := f.svc.syncLogRepo.CreateIfNoRunning(context.Background(), log)
	require.NoError(t, err)
	require.True(t, created)
	work, handled, err := feishusynccontrol.PrepareAutomaticSourceExecution(context.Background(),
		f.source.ID, log.ID, f.source.TenantID)
	require.NoError(t, err)
	require.True(t, handled)
	e, claimed, err := f.svc.syncLogRepo.(interfaces.SyncExecutionRepository).
		ClaimExecution(work, f.source.ID, log.ID, f.source.TenantID)
	require.NoError(t, err)
	require.True(t, claimed)
	work = types.WithSyncExecution(work, e)
	work = feishusafety.LegacyContext(work, f.source.Type, f.source.ID)
	fingerprint, err := feishusafety.SourceConfigFingerprint(f.source.ID, f.source.KnowledgeBaseID,
		f.source.Type, f.source.LegacyAccessState, f.source.TenantID, f.source.Config)
	require.NoError(t, err)
	work, _, err = feishusynccontrol.BindApprovedSource(work, f.source.ID, fingerprint)
	require.NoError(t, err)
	return feishusafety.WithRecoveryExecution(work, fingerprint, log.ID, e.Generation)
}

func TestFeishuWorkerIdentityProcessSyncUsesGuardedOwnerCredential(t *testing.T) {
	f := newWorkerSourceIdentityFixture(t)
	f.processSync(t)
	require.Zero(t, f.tokens.Load(), "personal OAuth must not obtain an application token")
}

func TestFeishuWorkerIdentityApplicationGrantUsesGuardedTenantToken(t *testing.T) {
	f := newWorkerSourceIdentityFixture(t, types.FeishuApplicationGrant)
	f.processSync(t)
	require.Positive(
		t,
		f.tokens.Load(),
		"the actual app provider must obtain a tenant token through the Guard",
	)
	require.Greater(t, f.remote.Load(), f.tokens.Load(), "token acquisition alone is not metadata access")
	owner, err := types.PrivateKBOwner(f.db, f.source.KnowledgeBaseID)
	require.NoError(t, err)
	require.Empty(
		t,
		owner,
		"application source uses a real nonprivate KB, never an exception to private isolation",
	)
}

func (f *workerSourceIdentityFixture) processSync(t *testing.T) {
	t.Helper()
	var sourceOwner types.User
	require.NoError(t, f.db.First(&sourceOwner, "id=?", f.user.ID).Error)
	require.False(t, sourceOwner.IsSystemAdmin)
	var membership types.TenantMember
	require.NoError(
		t,
		f.db.First(&membership, "user_id=? AND tenant_id=?", f.user.ID, f.source.TenantID).Error,
	)
	require.Equal(t, types.TenantRoleContributor, membership.Role)
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: f.source.ID,
		TenantID: f.source.TenantID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	created, err := f.svc.syncLogRepo.CreateIfNoRunning(context.Background(), log)
	require.NoError(t, err)
	require.True(t, created)
	body, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: f.source.ID,
		SyncLogID:    log.ID, TenantID: f.source.TenantID,
		Initiator: types.TaskInitiator{UserID: "audit-only-not-an-owner", Role: types.TenantRoleViewer},
	})
	require.NoError(t, err)
	err = f.svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, body))
	if err != nil {
		var api *core.APIError
		t.Logf("isolated ProcessSync transport_class=%s remote_requests=%d api_error=%t "+
			"recovery_rejected=%t safety_closed=%t", core.TransportErrorCategory(err), f.remote.Load(),
			errors.As(err, &api), errors.Is(err, feishusynccontrol.ErrRecoveryRejected),
			errors.Is(err, feishusafety.ErrClosed))
	}
	require.NoError(t, err)
	require.GreaterOrEqual(
		t,
		f.remote.Load(),
		int32(2),
		"real guarded Wiki and metadata HTTP must reach the fixture",
	)
	var settled types.SyncLog
	require.NoError(t, f.db.First(&settled, "id=?", log.ID).Error)
	require.Equal(t, types.SyncLogStatusSuccess, settled.Status)
	require.False(t, settled.ExecutionClaimed)
	require.EqualValues(t, 1, settled.ExecutionGeneration)
}

func TestFeishuWorkerIdentityRejectsMixedOrStaleAuthorityBeforeHTTP(t *testing.T) {
	for _, mode := range []string{
		"web", "invalid_principal", "key", "wrong_generation", "fake_claim", "unclaimed",
		"cross_owner", "membership_revoked", "kb_deleted",
	} {
		t.Run(mode, func(t *testing.T) {
			f := newWorkerSourceIdentityFixture(t)
			work := f.claim(t)
			switch mode {
			case "web":
				work = types.WithPrincipal(work, types.Principal{Type: types.PrincipalWebUser, ID: f.user.ID})
			case "invalid_principal":
				work = context.WithValue(work, types.PrincipalContextKey, "not-a-valid-principal")
			case "key":
				work = types.WithTenantAPIKeyScope(work, types.TenantAPIKeyScope{FullAccess: true})
			case "wrong_generation":
				e, _ := types.SyncExecutionFromContext(work)
				e.Generation++
				work = types.WithSyncExecution(work, e)
			case "fake_claim":
				e, _ := types.SyncExecutionFromContext(work)
				e.SyncLogID = uuid.NewString()
				work = types.WithSyncExecution(work, e)
			case "unclaimed":
				e, _ := types.SyncExecutionFromContext(work)
				require.NoError(t, f.db.Model(&types.SyncLog{}).Where("id=?", e.SyncLogID).
					Update("execution_claimed", false).Error)
			case "cross_owner":
				other := &types.User{
					ID: uuid.NewString(), Username: "other-owner", Email: "other@example.invalid",
					PasswordHash: "no-real-login", TenantID: f.source.TenantID, IsActive: true,
				}
				require.NoError(t, f.db.Create(other).Error)
				require.NoError(t, f.db.Exec("UPDATE feishu_private_kbs SET owner_id=? WHERE kb_id=?",
					other.ID, f.source.KnowledgeBaseID).Error)
			case "membership_revoked":
				require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id=?", f.user.ID).
					Update("status", types.TenantMemberStatusSuspended).Error)
			case "kb_deleted":
				require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id=?", f.source.KnowledgeBaseID).
					Update("deleted_at", time.Now().UTC()).Error)
			}
			_, err := f.svc.bindFeishuSource(work, f.source)
			require.Error(t, err)
			require.Zero(
				t,
				f.remote.Load(),
				"rejected authority must not send even the first source HTTP request",
			)
		})
	}
}

func TestFeishuWorkerIdentityDoesNotBypassRemoteRefusal(t *testing.T) {
	f := newWorkerSourceIdentityFixture(t)
	work := f.claim(t)
	require.NoError(t, feishusafety.Check(work))
	f.deny.Store(true)
	_, err := f.svc.bindFeishuSource(work, f.source)
	var api *core.APIError
	require.ErrorAs(t, err, &api)
	require.Equal(t, core.ErrorCategoryPermission, api.Category)
	require.Equal(t, 131006, api.Code)
	require.Equal(t, "get_wiki_node", api.Operation)
	require.ErrorIs(t, err, core.ErrPermission)
	require.Positive(t, f.remote.Load(), "remote refusal must remain an actual wire response")
	require.Zero(t, f.tokens.Load(), "a personal permission refusal never selects application credentials")
}
