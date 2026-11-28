package service

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
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type reparseDeliveryKnowledge struct {
	interfaces.KnowledgeService
	repo interfaces.KnowledgeRepository
}

func (k reparseDeliveryKnowledge) GetRepository() interfaces.KnowledgeRepository { return k.repo }

// Real SQL source/identity/intent and the production automatic admission code.
// Miniredis and a capturing queue exercise admission, not outbound execution.
func TestSourceDocumentReparseAutomaticDeliveryPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	for key, value := range map[string]string{
		"FEISHU_SYNC_SAFETY_ENABLED": "true", "FEISHU_APPROVED_SYNC_LANE_ENABLED": "1",
		"FEISHU_SAFE_INCREMENTAL_ENABLED": "true", "FEISHU_LEGACY_SYNC_SUSPENDED": "false",
		"FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS": "all",
	} {
		t.Setenv(key, value)
	}
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	tenant := types.Tenant{Name: "reparse-delivery-fixture", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	owner := types.User{
		ID: uuid.NewString(), Username: uuid.NewString(),
		Email: uuid.NewString() + "@example.invalid", PasswordHash: "synthetic",
		TenantID: tenant.ID, IsActive: true,
	}
	require.NoError(t, db.Create(&owner).Error)
	member := types.TenantMember{
		UserID: owner.ID, TenantID: tenant.ID,
		Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive,
	}
	require.NoError(t, db.Create(&member).Error)
	kb := types.KnowledgeBase{
		ID: uuid.NewString(), TenantID: tenant.ID,
		CreatorID: owner.ID, Name: "reparse-delivery-fixture", Type: "document",
	}
	require.NoError(t, db.Create(&kb).Error)
	ds := types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive, LegacyAccessState: "active",
		Config:       types.JSON(`{"resource_ids":["fixture-root"],"settings":{}}`),
		SyncSchedule: "0 0 2 * * *", LastSyncCursor: types.JSON(`{"fixture":"unchanged"}`),
	}
	require.NoError(t, db.Create(&ds).Error)
	// Fingerprints use the persisted JSONB representation, as normal requests do.
	require.NoError(t, db.First(&ds, "id=?", ds.ID).Error)
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	guard, err := feishusafety.New(rdb, feishusafety.Config{
		Namespace: "reparse-delivery-" + uuid.NewString(), EndpointHost: "gateway.invalid",
		InFlight: 1, RPM: 30, RPS: 1, TPM: 30000, WaitBudgetDuration: time.Second, TokenBoundVerified: true,
	})
	require.NoError(t, err)
	control, err := feishusynccontrol.New(rdb, guard, feishusynccontrol.Config{
		Automatic: true, AutomaticService: true, DedicatedSyncLane: true, Scope: "all",
		WorkerID: "reparse-delivery-test:1", SourceSHA: strings.Repeat("a", 40), Maintenance: 4,
	}, func(context.Context) error { return nil }, func(feishusynccontrol.Event) error { return nil })
	require.NoError(t, err)
	manager, err := feishusynccontrol.NewAutomaticServiceManager(control, db,
		repository.NewTenantAPIKeyRepository(db).(feishusynccontrol.AutomaticSourceKeyScopes))
	require.NoError(t, err)
	priorManager, priorGuard := feishusynccontrol.Recovery(), feishusafety.Default()
	feishusynccontrol.InstallRecovery(manager)
	feishusafety.InstallDefault(guard)
	t.Cleanup(func() { feishusynccontrol.InstallRecovery(priorManager); feishusafety.InstallDefault(priorGuard) })
	svc := &DataSourceService{
		dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: repository.NewSyncLogRepository(db),
		knowledgeService: reparseDeliveryKnowledge{repo: repository.NewKnowledgeRepository(db)},
		feishuIdentity:   &FeishuPersonalService{db: db},
	}
	request := types.FeishuDocumentReparse{
		ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		KnowledgeID: uuid.NewString(), RootID: "fixture-root", ExternalID: "synthetic-document", FileType: "xlsx",
		Initiator:  types.TaskInitiator{UserID: owner.ID, Role: types.TenantRoleOwner},
		ConfigHash: repository.LegacySourceReparseConfigHash(&ds), RequestedAt: time.Now().UTC(),
	}
	// Synthetic accepted row, without parser output or a published Knowledge.
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	op := types.TaskPendingOp{
		TenantID: tenant.ID, TaskType: "feishu:document:reparse",
		Scope: "feishu_document_reparse_v1", ScopeID: ds.ID, DedupKey: request.KnowledgeID,
		Op: "queued", Payload: payload, EnqueuedAt: request.RequestedAt,
	}
	require.NoError(t, db.Create(&op).Error)
	background := types.WithExecutionTenant(request.Initiator.Apply(context.Background()), tenant.ID)
	var tasks []*asynq.Task
	var queues []string
	enqueue := func(task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
		tasks = append(tasks, task)
		// The production path embeds queue defaults in NewTask; enqueue options
		// override them. Capture both just as the real client resolves them.
		for _, option := range append(task.Options(), options...) {
			if option.Type() == asynq.QueueOpt {
				queues = append(queues, option.Value().(string))
			}
		}
		return &asynq.TaskInfo{ID: uuid.NewString()}, nil
	}
	_, err = svc.manualSyncUsing(background, ds.ID, enqueue)
	require.Error(t, err, "reproduce: restored audit identity is not a live Web principal")
	require.Empty(t, tasks)
	var logs int64
	require.NoError(t, db.Model(&types.SyncLog{}).Where("data_source_id=?", ds.ID).Count(&logs).Error)
	require.Zero(t, logs)
	store := repository.NewFeishuLegacyStore(db)
	delivery, err := store.PrepareSourceDocumentReparseDelivery(background, &ds, &request)
	require.NoError(t, err)
	delivery = context.WithValue(delivery, documentReparseRunKey{}, true)
	log, err := svc.manualSyncUsing(delivery, ds.ID, enqueue)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, []string{types.QueueApprovedSync}, queues)
	var queued types.DataSourceSyncPayload
	require.NoError(t, json.Unmarshal(tasks[0].Payload(), &queued))
	require.Equal(t, "document_reparse", queued.Trigger)
	require.False(t, queued.ForceFull)
	require.Equal(t, request.Initiator, queued.Initiator)
	require.Equal(t, log.ID, queued.SyncLogID)
	require.Nil(t, delivery.Value(types.PrincipalContextKey))
	require.Zero(t, rdb.Exists(context.Background(), guard.PermitKey()).Val(),
		"queueing cannot issue an outbound permit")
	_, err = svc.manualSyncUsing(delivery, ds.ID, enqueue)
	require.NoError(t, err)
	require.Len(t, tasks, 1, "existing running log coalesces duplicate wake-ups")
	web := types.WithPrincipal(delivery, types.Principal{Type: types.PrincipalWebUser, ID: owner.ID})
	_, err = svc.manualSyncUsing(web, ds.ID, enqueue)
	require.Error(t, err, "an HTTP request cannot reuse the scheduler continuation")
	require.NoError(t, db.Model(&member).Where("user_id=? AND tenant_id=?", owner.ID, tenant.ID).
		Update("role", types.TenantRoleViewer).Error)
	_, err = svc.manualSyncUsing(delivery, ds.ID, enqueue)
	require.Error(t, err, "live management revocation stops an accepted request")
	require.NoError(t, db.Model(&member).Where("user_id=? AND tenant_id=?", owner.ID, tenant.ID).
		Update("role", types.TenantRoleOwner).Error)
	require.NoError(t, db.Model(&op).UpdateColumn("op", "completed").Error)
	_, err = svc.manualSyncUsing(delivery, ds.ID, enqueue)
	require.Error(t, err, "completed intent cannot admit another wake-up")
	require.Len(t, tasks, 1)
	var saved types.DataSource
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	require.Equal(t, ds.SyncSchedule, saved.SyncSchedule)
	require.JSONEq(t, string(ds.LastSyncCursor), string(saved.LastSyncCursor))
}
