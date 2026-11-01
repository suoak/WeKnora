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

func TestSteadyAuthorityPostgresCrossTenantAndPrivateOwner(t *testing.T) {
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
	ctx := context.Background()
	a := types.Tenant{Name: "steady-a", Status: "active"}
	b := types.Tenant{Name: "steady-b", Status: "active"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	actor := types.User{ID: uuid.NewString(), Username: "steady-actor", Email: uuid.NewString() + "@example.invalid",
		PasswordHash: "synthetic", TenantID: a.ID, IsActive: true, IsSystemAdmin: true}
	require.NoError(t, db.Create(&actor).Error)
	member := types.TenantMember{UserID: actor.ID, TenantID: a.ID,
		Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive}
	require.NoError(t, db.Create(&member).Error)
	makeSource := func(tenant uint64) types.DataSource {
		kb := types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant, Name: "steady", Type: "document"}
		require.NoError(t, db.Create(&kb).Error)
		ds := types.DataSource{ID: uuid.NewString(), TenantID: tenant, KnowledgeBaseID: kb.ID, Type: "feishu_drive",
			Name: "steady", Status: "active", LegacyAccessState: "unmanaged",
			Config: types.JSON(`{"resource_ids":["synthetic-root"],"settings":{}}`)}
		require.NoError(t, db.Create(&ds).Error)
		return ds
	}
	one, two := makeSource(a.ID), makeSource(b.ID)
	auth, err := SourceManagementFromDatabase(ctx, db, one.ID, actor.ID)
	require.NoError(t, err)
	require.Equal(t, a.ID, auth.TenantID)
	_, err = SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.Error(t, err, "system admin is not a cross-space grant")
	second := types.TenantMember{UserID: actor.ID, TenantID: b.ID,
		Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive}
	require.NoError(t, db.Create(&second).Error)
	_, err = SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.Error(t, err, "viewer is not a source manager")
	require.NoError(t, db.Model(&second).Update("role", "admin").Error)
	auth, err = SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.NoError(t, err)
	require.Equal(t, b.ID, auth.TenantID)
	other := types.User{ID: uuid.NewString(), Username: "steady-private-owner",
		Email:        uuid.NewString() + "@example.invalid",
		PasswordHash: "synthetic", TenantID: b.ID, IsActive: true}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Exec(`INSERT INTO feishu_private_kbs (kb_id,owner_id) VALUES (?,?)`,
		two.KnowledgeBaseID, other.ID).Error)
	_, err = SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.Error(t, err, "even a real tenant admin cannot take another private KB")
	require.NoError(t, db.Exec(`UPDATE feishu_private_kbs SET owner_id=? WHERE kb_id=?`,
		actor.ID, two.KnowledgeBaseID).Error)
	owned, err := SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.NoError(t, err)
	require.NotEqual(t, auth.AuthorityHash, owned.AuthorityHash)
	require.NoError(t, db.Model(&second).Update("status", "inactive").Error)
	_, err = SourceManagementFromDatabase(ctx, db, two.ID, actor.ID)
	require.Error(t, err, "revoked membership is immediate")
	require.NoError(t, db.Model(&actor).Update("is_active", false).Error)
	_, err = SourceManagementFromDatabase(ctx, db, one.ID, actor.ID)
	require.Error(t, err, "deactivated managers cannot keep renewing authority")
}
