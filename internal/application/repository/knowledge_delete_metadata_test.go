package repository

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
)

func TestDeletionMetadataPostgresPreservesSourceFenceAndContent(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	if dsn == "" {
		t.Skip("requires disposable feishu_atomic_test PostgreSQL")
	}
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tenant := &types.Tenant{ID: 872501, Name: "delete-test", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "held"}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Name: "held", Type: "feishu_drive", Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	k := &types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Title: "metadata", Description: "body-derived summary must not be read or overwritten",
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", StorageSize: 123, Metadata: types.JSON(`{"keep":"original"}`),
	}
	require.NoError(t, db.Create(k).Error)
	db = types.EnableLegacyReadGuards(db)
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()
	_, err = repo.GetKnowledgeBatch(ctx, tenant.ID, []string{k.ID})
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	rows, err := repo.GetKnowledgeDeletionMetadata(ctx, tenant.ID, []string{k.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].Description)
	require.Nil(t, rows[0].Profile)
	require.Empty(t, rows[0].CustomMetadata)
	wrongTenant, err := repo.GetKnowledgeDeletionMetadata(ctx, tenant.ID+1, []string{k.ID})
	require.NoError(t, err)
	require.Empty(t, wrongTenant)
	stale := *rows[0]
	require.NoError(t, repo.MarkKnowledgeDeleting(ctx, rows[0]))
	require.Error(t, repo.MarkKnowledgeDeleting(ctx, &stale))
	var stored struct {
		Description  string
		EnableStatus string
		StorageSize  int64
		Metadata     types.JSON
	}
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", k.ID).Scan(&stored).Error)
	require.Equal(t, k.Description, stored.Description)
	require.Equal(t, k.EnableStatus, stored.EnableStatus)
	require.Equal(t, k.StorageSize, stored.StorageSize)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata))
	_, err = repo.GetKnowledgeBatch(ctx, tenant.ID, []string{k.ID})
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	var sourceState string
	require.NoError(t, db.Raw("SELECT legacy_access_state FROM data_sources WHERE id=?", ds.ID).Scan(&sourceState).Error)
	require.Equal(t, "hold", sourceState)
}
