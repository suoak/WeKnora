package feishusynccontrol

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestRecoveryFactsPostgresAuthorityAndExecutionChanges(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	ctx := context.Background()
	tenant := types.Tenant{Name: "isolated-recovery-facts", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	user := types.User{ID: uuid.NewString(), Username: "isolated-recovery", Email: uuid.NewString() + "@example.invalid", PasswordHash: "synthetic", TenantID: tenant.ID, IsActive: true}
	require.NoError(t, db.Create(&user).Error)
	member := types.TenantMember{UserID: user.ID, TenantID: tenant.ID, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}
	require.NoError(t, db.Create(&member).Error)
	kb := types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "facts", Type: "document"}
	require.NoError(t, db.Create(&kb).Error)
	source := types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Type: "feishu_drive", Name: "facts", Status: "active", LegacyAccessState: "unmanaged", Config: types.JSON(`{"resource_ids":["bounded-root"],"settings":{}}`)}
	require.NoError(t, db.Create(&source).Error)
	initial, err := RecoveryFactsFromDatabase(ctx, db, source.ID)
	require.NoError(t, err)
	require.True(t, initial.Eligible)
	require.Equal(t, 1, initial.SelectedResourceCount)
	require.NoError(t, db.Model(&user).Update("is_active", false).Error)
	changed, err := RecoveryFactsFromDatabase(ctx, db, source.ID)
	require.NoError(t, err)
	require.NotEqual(t, initial.FactsSHA256, changed.FactsSHA256)
	require.NoError(t, db.Model(&source).Update("legacy_access_state", "revoked").Error)
	changed, err = RecoveryFactsFromDatabase(ctx, db, source.ID)
	require.NoError(t, err)
	require.False(t, changed.Eligible)
	require.NotEqual(t, initial.ConfigSHA256, changed.ConfigSHA256)
	execution := types.SyncLog{ID: uuid.NewString(), DataSourceID: source.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true}
	require.NoError(t, db.Create(&execution).Error)
	changed, err = RecoveryFactsFromDatabase(ctx, db, source.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, changed.LiveExecutions)
	_, err = RecoveryFactsFromDatabase(ctx, db, uuid.NewString())
	require.Error(t, err)
}
