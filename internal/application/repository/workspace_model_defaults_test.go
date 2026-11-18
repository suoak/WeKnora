package repository

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net/url"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestWorkspaceDefaultsAtomicAuditConflictAndRollback(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	tenant := &types.Tenant{Name: "model-defaults", Description: "preserve unrelated settings"}
	require.NoError(t, db.Create(tenant).Error)
	repo := NewTenantRepository(db)
	empty := &types.DefaultSettings{}
	next := &types.DefaultSettings{ChatModelID: "enterprise-chat", EmbeddingModelID: "embedding-v2"}
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin-1")
	require.NoError(t, repo.UpdateDefaultSettings(ctx, tenant.ID, next, empty))
	// A second editor cannot overwrite the first editor's committed change.
	assert.ErrorIs(t, repo.UpdateDefaultSettings(ctx, tenant.ID, empty, empty), types.ErrDefaultSettingsConflict)
	loaded, err := repo.GetTenantByID(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, next, loaded.DefaultSettings)
	assert.Equal(t, tenant.Description, loaded.Description)
	var entries []types.AuditLog
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 1)
	assert.Equal(t, "admin-1", entries[0].ActorUserID)
	assert.Contains(t, string(entries[0].Details), `"old_value"`)
	// Rollback uses the same validation/CAS path and produces another audit.
	require.NoError(t, repo.UpdateDefaultSettings(ctx, tenant.ID, empty, next))
	loaded, err = repo.GetTenantByID(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, empty, loaded.DefaultSettings)
	require.NoError(t, db.Find(&entries).Error)
	assert.Len(t, entries, 2)
}

func TestWorkspaceDefaultsAuditFailureRollsBackSettings(t *testing.T) {
	db := setupTestDB(t) // Deliberately no audit table.
	tenant := &types.Tenant{Name: "audit-failure"}
	require.NoError(t, db.Create(tenant).Error)
	repo := NewTenantRepository(db)
	err := repo.UpdateDefaultSettings(context.Background(), tenant.ID, &types.DefaultSettings{ChatModelID: "chat"}, &types.DefaultSettings{})
	require.Error(t, err)
	loaded, err := repo.GetTenantByID(context.Background(), tenant.ID)
	require.NoError(t, err)
	assert.Nil(t, loaded.DefaultSettings)
}

// The dedicated DSN must point to a disposable acceptance database. All
// fixtures live in a fresh schema and never read any existing tenant rows.
func TestWorkspaceDefaultsPostgresCASAndAudit(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MODEL_DEFAULTS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL acceptance database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "model_defaults_" + uuid.NewString()[:8]
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() {
		_ = db.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	scoped, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB, _ := scoped.DB(); _ = sqlDB.Close() })
	require.NoError(t, scoped.AutoMigrate(&types.Tenant{}, &types.AuditLog{}))
	tenant := &types.Tenant{Name: "postgres-defaults"}
	require.NoError(t, scoped.Create(tenant).Error)
	repo := NewTenantRepository(scoped)
	previous := &types.DefaultSettings{}
	for _, id := range []string{"chat-a", "chat-b", ""} {
		next := &types.DefaultSettings{ChatModelID: id}
		require.NoError(t, repo.UpdateDefaultSettings(context.Background(), tenant.ID, next, previous))
		previous = next
	}
	require.ErrorIs(t, repo.UpdateDefaultSettings(context.Background(), tenant.ID, &types.DefaultSettings{}, &types.DefaultSettings{ChatModelID: "old"}), types.ErrDefaultSettingsConflict)
	var total int64
	require.NoError(t, scoped.Model(&types.AuditLog{}).Count(&total).Error)
	require.EqualValues(t, 3, total)
	loaded, err := repo.GetTenantByID(context.Background(), tenant.ID)
	require.NoError(t, err)
	require.Equal(t, previous, loaded.DefaultSettings)
	t.Log(fmt.Sprintf("PostgreSQL audited save, replacement, rollback and stale snapshot refusal passed in %s", schema))
}
