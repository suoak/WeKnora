package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSourceRestrictionPostgres(t *testing.T) {
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
	testSourceRestrictionDB(t, db)
}

func TestSourceRestrictionSQLite(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "admission.db"))
	require.NoError(t, database.RunMigrations("sqlite3://"+path))
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	testSourceRestrictionDB(t, db)
}

func testSourceRestrictionDB(t *testing.T, db *gorm.DB) {
	tenant := &types.Tenant{ID: 809932, Name: "source-admission-isolation", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "held", Type: "document", EmbeddingModelID: model.ID}
	other := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "normal", Type: "document", EmbeddingModelID: model.ID}
	require.NoError(t, db.Create(kb).Error)
	require.NoError(t, db.Create(other).Error)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "pending", Type: "file", Channel: "web", ParseStatus: types.ParseStatusPending, Metadata: types.JSON(`{}`)}
	require.NoError(t, db.Create(k).Error)
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "restricted", Type: "feishu_drive", Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`)}
	require.NoError(t, db.Create(ds).Error)
	db = types.EnableLegacyReadGuards(db)
	ctx := context.Background()
	got, err := knowledgeSourceRestriction(ctx, db, tenant.ID, kb.ID, k.ID)
	require.NoError(t, err)
	require.True(t, got.Restricted)
	require.Equal(t, "hold", got.SourceState)
	require.Equal(t, "unverified", got.HistoricalProcessingState)
	require.ErrorIs(t, types.CheckLegacySourceRead(db, k.ID, kb.ID), types.ErrLegacySourceRestricted)
	_, err = knowledgeSourceRestriction(ctx, db, tenant.ID+1, kb.ID, k.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = knowledgeSourceRestriction(ctx, db, tenant.ID, other.ID, k.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	normal, err := knowledgeSourceRestriction(ctx, db, tenant.ID, other.ID, "")
	require.NoError(t, err)
	require.False(t, normal.Restricted)
	require.NoError(t, db.Exec("UPDATE data_sources SET legacy_access_state='revoked',deleted_at=? WHERE id=?", time.Now().UTC(), ds.ID).Error)
	got, err = knowledgeSourceRestriction(ctx, db, tenant.ID, kb.ID, k.ID)
	require.NoError(t, err)
	require.True(t, got.Restricted)
	require.Equal(t, "revoked", got.SourceState)
	require.ErrorIs(t, types.CheckLegacySourceRead(db, k.ID, kb.ID), types.ErrLegacySourceRestricted)
	var status string
	require.NoError(t, db.Raw("SELECT parse_status FROM knowledges WHERE id=?", k.ID).Scan(&status).Error)
	require.Equal(t, string(types.ParseStatusPending), status, "operational status query never changes document lifecycle")
}
