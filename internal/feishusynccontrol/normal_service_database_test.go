package feishusynccontrol_test

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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestNormalSourcePostgresCurrentWritersClaimsAndPublicationRevocation(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer func() { require.NoError(t, pool.Close()) }()
	tenant := types.Tenant{Name: "normal-service", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	makeUser := func(role types.TenantRole) (types.User, types.TenantMember) {
		user := types.User{
			ID: uuid.NewString(), Username: uuid.NewString(), Email: uuid.NewString() + "@example.invalid",
			PasswordHash: "synthetic", TenantID: tenant.ID, IsActive: true,
		}
		require.NoError(t, db.Create(&user).Error)
		member := types.TenantMember{
			UserID: user.ID, TenantID: tenant.ID, Role: role, Status: types.TenantMemberStatusActive,
		}
		require.NoError(t, db.Create(&member).Error)
		return user, member
	}
	owner, _ := makeUser(types.TenantRoleOwner)
	writer, membership := makeUser(types.TenantRoleContributor)
	viewer, _ := makeUser(types.TenantRoleViewer)
	// Authorization is verified by the manager before this storage boundary.
	// Verify durable CAS/history independently of the Redis certificate tests.
	record := feishusynccontrol.NormalServiceRecord{
		Authorization: feishusynccontrol.NormalServiceAuthorization{ID: uuid.NewString(), ActorID: owner.ID},
		Active:        true, ActivatedMS: time.Now().UnixMilli(),
	}
	require.NoError(t, feishusynccontrol.StoreNormalServiceRecord(context.Background(), db, record, nil))
	require.Error(t, feishusynccontrol.StoreNormalServiceRecord(context.Background(), db, record, nil))
	var setting types.SystemSetting
	require.NoError(t, db.First(&setting, "key=?", feishusynccontrol.NormalServiceSetting).Error)
	var persisted feishusynccontrol.NormalServiceRecord
	require.NoError(t, json.Unmarshal(setting.Value, &persisted))
	require.Equal(t, record, persisted)
	revocation := record
	revocation.Active = false
	require.NoError(t, feishusynccontrol.StoreNormalServiceRecord(context.Background(), db, revocation, &record))
	require.Error(t, feishusynccontrol.StoreNormalServiceRecord(context.Background(), db, record, &record))
	var histories int64
	require.NoError(t, db.Model(&types.SystemSetting{}).
		Where("key LIKE ?", feishusynccontrol.NormalServiceSetting+".history.%").Count(&histories).Error)
	require.EqualValues(t, 1, histories)
	makeSource := func(kind string) (types.DataSource, string) {
		kb := types.KnowledgeBase{
			ID: uuid.NewString(), TenantID: tenant.ID, Name: "normal-source", Type: "document", CreatorID: owner.ID,
		}
		require.NoError(t, db.Create(&kb).Error)
		ds := types.DataSource{
			ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Type: kind,
			Status: "active", LegacyAccessState: "unmanaged",
			Config: types.JSON(`{"resource_ids":["synthetic-root"],"settings":{}}`),
		}
		require.NoError(t, db.Create(&ds).Error)
		fingerprint, err := feishusafety.SourceConfigFingerprint(
			ds.ID, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.TenantID, ds.Config)
		require.NoError(t, err)
		return ds, fingerprint
	}
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, writer.ID)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: tenant.ID, UserID: writer.ID, Role: types.TenantRoleContributor})
	var source types.DataSource
	var config string
	for _, kind := range []string{"feishu", "feishu_drive"} {
		source, config = makeSource(kind)
		binding, err := feishusynccontrol.NormalSourceFromDatabase(ctx, db, nil, source.ID, config)
		require.NoError(t, err, "a current writer does not need a source-ID approval")
		require.Equal(t, tenant.ID, binding.TenantID)
		_, err = feishusynccontrol.NormalSourceFromDatabase(context.Background(), db, nil, source.ID, config)
		require.NoError(t, err, "existing space management authorizes the enterprise background job")
	}
	viewerCtx := context.WithValue(context.Background(), types.UserIDContextKey, viewer.ID)
	_, err = feishusynccontrol.NormalSourceFromDatabase(viewerCtx, db, nil, source.ID, config)
	require.NoError(t, err, "every active workspace member may manage a source")
	cross := types.WithCaller(ctx, types.Caller{
		TenantID: tenant.ID + 999, UserID: writer.ID, Role: types.TenantRoleOwner,
	})
	_, err = feishusynccontrol.NormalSourceFromDatabase(cross, db, nil, source.ID, config)
	require.Error(t, err)
	_, err = feishusynccontrol.NormalSourceFromDatabase(ctx, db, nil, source.ID, strings.Repeat("f", 64))
	require.Error(t, err)
	logs := repository.NewSyncLogRepository(db)
	log := &types.SyncLog{DataSourceID: source.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning}
	created, err := logs.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, created)
	execution, claimed, err := logs.(interfaces.SyncExecutionRepository).
		ClaimExecution(ctx, source.ID, log.ID, tenant.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	claimedCtx := types.WithSyncExecution(ctx, execution)
	_, err = feishusynccontrol.NormalSourceFromDatabase(claimedCtx, db, nil, source.ID, config)
	require.NoError(t, err)
	stale := execution
	stale.Generation++
	_, err = feishusynccontrol.NormalSourceFromDatabase(types.WithSyncExecution(ctx, stale), db, nil, source.ID, config)
	require.Error(t, err)
	require.NoError(t, db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES (?,?)",
		source.KnowledgeBaseID, owner.ID).Error)
	_, err = feishusynccontrol.NormalSourceFromDatabase(claimedCtx, db, nil, source.ID, config)
	require.Error(t, err, "space write permission cannot take another user's private KB")
	require.NoError(t, db.Exec("DELETE FROM feishu_private_kbs WHERE kb_id=?", source.KnowledgeBaseID).Error)
	ready, release, committed := make(chan error, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		committed <- db.Transaction(func(tx *gorm.DB) error {
			_, err := feishusynccontrol.NormalSourceFromDatabase(claimedCtx, db, tx, source.ID, config)
			ready <- err
			<-release
			return err
		})
	}()
	require.NoError(t, <-ready)
	revoked := make(chan error, 1)
	go func() { revoked <- db.Model(&membership).Update("status", "inactive").Error }()
	select {
	case err := <-revoked:
		close(release)
		t.Fatalf("membership revocation crossed the publication authority lock: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-committed)
	require.NoError(t, <-revoked)
	_, err = feishusynccontrol.NormalSourceFromDatabase(claimedCtx, db, nil, source.ID, config)
	require.Error(t, err, "revoked writers cannot continue publishing")
}
