package datasource

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// Real SQL resource/intent test. Miniredis and this capturing adapter cover only
// admission and queue selection, never actual consumer health or connector I/O.
type schedulerAutomaticQueue struct {
	tasks  []*asynq.Task
	queues []string
}

func (q *schedulerAutomaticQueue) Enqueue(task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	for _, option := range options {
		if option.Type() == asynq.QueueOpt {
			q.queues = append(q.queues, option.Value().(string))
		}
	}
	return &asynq.TaskInfo{ID: uuid.NewString()}, nil
}

type schedulerAutomaticFixture struct {
	db        *gorm.DB
	tenant    types.Tenant
	owner     types.User
	kb        types.KnowledgeBase
	guard     *feishusafety.Guard
	redis     *redis.Client
	queue     *schedulerAutomaticQueue
	scheduler *Scheduler
}

func newSchedulerAutomaticFixture(t *testing.T) schedulerAutomaticFixture {
	t.Helper()
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", "all")
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	f := schedulerAutomaticFixture{db: db, tenant: types.Tenant{Name: "scheduler-automatic-fixture", Status: "active"}}
	require.NoError(t, db.Create(&f.tenant).Error)
	f.owner = types.User{ID: uuid.NewString(), Username: uuid.NewString(), Email: uuid.NewString() + "@example.invalid", PasswordHash: "synthetic", TenantID: f.tenant.ID, IsActive: true}
	require.NoError(t, db.Create(&f.owner).Error)
	require.NoError(t, db.Create(&types.TenantMember{UserID: f.owner.ID, TenantID: f.tenant.ID, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
	f.kb = types.KnowledgeBase{ID: uuid.NewString(), TenantID: f.tenant.ID, CreatorID: f.owner.ID, Name: "scheduler-automatic-test", Type: "document"}
	require.NoError(t, db.Create(&f.kb).Error)
	mini := miniredis.RunT(t)
	f.redis = redis.NewClient(&redis.Options{Addr: mini.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = f.redis.Close() })
	f.guard, err = feishusafety.New(f.redis, feishusafety.Config{Namespace: "scheduler-automatic-" + uuid.NewString(), EndpointHost: "gateway.invalid", InFlight: 1, RPM: 30, RPS: 1, TPM: 30000, WaitBudgetDuration: time.Second, TokenBoundVerified: true})
	require.NoError(t, err)
	control, err := feishusynccontrol.New(f.redis, f.guard, feishusynccontrol.Config{Automatic: true, AutomaticService: true, DedicatedSyncLane: true, Scope: "all", WorkerID: "scheduler-test:1", SourceSHA: strings.Repeat("a", 40), Maintenance: 4}, func(context.Context) error { return nil }, func(feishusynccontrol.Event) error { return nil })
	require.NoError(t, err)
	manager, err := feishusynccontrol.NewAutomaticServiceManager(control, db, repository.NewTenantAPIKeyRepository(db).(feishusynccontrol.AutomaticSourceKeyScopes))
	require.NoError(t, err)
	priorRecovery, priorGuard := feishusynccontrol.Recovery(), feishusafety.Default()
	feishusynccontrol.InstallRecovery(manager)
	feishusafety.InstallDefault(f.guard)
	t.Cleanup(func() { feishusynccontrol.InstallRecovery(priorRecovery); feishusafety.InstallDefault(priorGuard) })
	f.queue = &schedulerAutomaticQueue{}
	f.scheduler = NewScheduler(repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db), f.queue)
	t.Log("SQL: disposable PostgreSQL with full migrations; Redis: miniredis admission fixture; queue: capturing adapter, no consumer or outbound requests")
	return f
}

func (f schedulerAutomaticFixture) source(t *testing.T) types.DataSource {
	t.Helper()
	ds := types.DataSource{ID: uuid.NewString(), TenantID: f.tenant.ID, KnowledgeBaseID: f.kb.ID, Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive, LegacyAccessState: "unmanaged", Config: types.JSON(`{"resource_ids":["fixture-root"],"settings":{}}`), SyncSchedule: "0 0 0 * * *", LastSyncCursor: types.JSON(`{"fixture":"unchanged"}`)}
	require.NoError(t, f.db.Create(&ds).Error)
	return ds
}

func (f schedulerAutomaticFixture) logs(t *testing.T, source string) []types.SyncLog {
	t.Helper()
	var logs []types.SyncLog
	require.NoError(t, f.db.Where("data_source_id=?", source).Find(&logs).Error)
	return logs
}

func (f schedulerAutomaticFixture) closed(t *testing.T, source string) {
	t.Helper()
	require.Zero(t, f.redis.Exists(context.Background(), f.guard.PermitKey()).Val())
	require.Error(t, f.guard.Check(feishusafety.WithScope(context.Background(), source)), "queue admission is not an execution grant")
}

func TestSchedulerAutomaticServicePersistsIntentAndApprovedQueue(t *testing.T) {
	f := newSchedulerAutomaticFixture(t)
	ds := f.source(t)
	f.scheduler.triggerSync(ds.ID, ds.TenantID)
	require.Len(t, f.queue.tasks, 1)
	require.Equal(t, []string{types.QueueApprovedSync}, f.queue.queues)
	var payload types.DataSourceSyncPayload
	require.NoError(t, json.Unmarshal(f.queue.tasks[0].Payload(), &payload))
	require.Equal(t, ds.ID, payload.DataSourceID)
	require.Equal(t, ds.TenantID, payload.TenantID)
	require.Equal(t, "schedule", payload.Trigger)
	require.False(t, payload.ForceFull)
	logs := f.logs(t, ds.ID)
	require.Len(t, logs, 1)
	require.Equal(t, payload.SyncLogID, logs[0].ID)
	require.Equal(t, types.SyncLogStatusRunning, logs[0].Status)
	require.False(t, logs[0].ExecutionClaimed)
	require.Zero(t, logs[0].ExecutionGeneration)
	f.closed(t, ds.ID)
	f.scheduler.triggerSync(ds.ID, ds.TenantID)
	require.Len(t, f.queue.tasks, 1, "durable running intent prevents overlapping schedule ticks")
	require.Len(t, f.logs(t, ds.ID), 1)
	var saved types.DataSource
	require.NoError(t, f.db.First(&saved, "id=?", ds.ID).Error)
	require.JSONEq(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
	fingerprint, err := feishusafety.SourceConfigFingerprint(ds.ID, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.TenantID, ds.Config)
	require.NoError(t, err)
	http := context.WithValue(context.Background(), types.UserIDContextKey, f.owner.ID)
	http = types.WithCaller(http, types.Caller{UserID: f.owner.ID, TenantID: f.tenant.ID, Role: types.TenantRoleOwner})
	http = types.WithPrincipal(http, types.Principal{Type: types.PrincipalWebUser, ID: f.owner.ID})
	handled, err := feishusynccontrol.QueueScheduledSource(http, ds.ID, fingerprint)
	require.True(t, handled)
	require.Error(t, err, "HTTP principal cannot select the scheduler-only policy")
	require.Len(t, f.logs(t, ds.ID), 1)
	f.closed(t, ds.ID)
}

func TestSchedulerAutomaticServiceCurrentRevocationAndTenantBinding(t *testing.T) {
	f := newSchedulerAutomaticFixture(t)
	for _, scenario := range []string{"paused", "hold", "revoked-owner", "revoked-membership", "no-management", "tenant-inactive", "tenant-mismatch", "operator-stop"} {
		t.Run(scenario, func(t *testing.T) {
			ds := f.source(t)
			tenant := ds.TenantID
			switch scenario {
			case "paused":
				require.NoError(t, f.db.Model(&ds).Update("status", types.DataSourceStatusPaused).Error)
			case "hold":
				require.NoError(t, f.db.Model(&ds).Update("legacy_access_state", "hold").Error)
			case "revoked-owner":
				require.NoError(t, f.db.Model(&f.owner).Update("is_active", false).Error)
				defer f.db.Model(&f.owner).Update("is_active", true)
			case "revoked-membership":
				require.NoError(t, f.db.Table("tenant_members").Where("user_id=? AND tenant_id=?", f.owner.ID, f.tenant.ID).Update("status", "suspended").Error)
				defer f.db.Table("tenant_members").Where("user_id=? AND tenant_id=?", f.owner.ID, f.tenant.ID).Update("status", "active")
			case "no-management":
				require.NoError(t, f.db.Table("tenant_members").Where("user_id=? AND tenant_id=?", f.owner.ID, f.tenant.ID).Update("role", "viewer").Error)
				defer f.db.Table("tenant_members").Where("user_id=? AND tenant_id=?", f.owner.ID, f.tenant.ID).Update("role", "owner")
			case "tenant-inactive":
				require.NoError(t, f.db.Model(&f.tenant).Update("status", "suspended").Error)
				defer f.db.Model(&f.tenant).Update("status", "active")
			case "tenant-mismatch":
				tenant++
			case "operator-stop":
				require.NoError(t, f.guard.Trip(context.Background(), "operator_emergency_stop"))
			}
			before := len(f.queue.tasks)
			f.scheduler.triggerSync(ds.ID, tenant)
			require.Len(t, f.queue.tasks, before)
			require.Empty(t, f.logs(t, ds.ID), "rejected tick cannot leave a running or claimed intent")
			f.closed(t, ds.ID)
		})
	}
}
