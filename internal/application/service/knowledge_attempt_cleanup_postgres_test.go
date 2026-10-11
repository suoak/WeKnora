package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestA3ProcessingAttemptCleanupPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWHUB25_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("KNOWHUB25_REQUIRE_POSTGRES") == "1" {
			t.Fatal("required isolated PostgreSQL is absent")
		}
		t.Skip("isolated PostgreSQL is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "cleanup_" + uuid.NewString()[:8]
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		if connection, e := db.DB(); e == nil {
			_ = connection.Close()
		}
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		if connection, e := admin.DB(); e == nil {
			_ = connection.Close()
		}
	})
	// Synthetic, isolated fixture only; not the application migration runner.
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	chunks := repository.NewChunkRepository(db)
	service := &knowledgeService{chunkRepo: chunks}
	old := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	newer := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	other := &types.Chunk{ID: uuid.NewString(), TenantID: 2, KnowledgeID: "two", IsEnabled: true}
	require.NoError(t, chunks.CreateChunks(context.Background(), []*types.Chunk{old, newer, other}))
	knowledge := &types.Knowledge{ID: "one", TenantID: 1}
	vectorCalls := 0
	vectorDelete := func(ctx context.Context, ids []string) error {
		vectorCalls++
		require.NoError(t, ctx.Err())
		require.Equal(t, []string{old.ID}, ids)
		return errors.New("synthetic vector failure")
	}
	// A failed vector cleanup remains a failure despite successful SQL cleanup.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.cleanupAttemptChunks(canceled, knowledge, []*types.Chunk{old}, vectorDelete), errAttemptCleanupFailed)
	require.Equal(t, 1, vectorCalls)
	var count int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id IN ?", []string{newer.ID, other.ID}).Count(&count).Error)
	require.Equal(t, int64(2), count)
	// Retrying the same exact IDs is safe and does not include the new generation.
	require.NoError(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{old}, func(ctx context.Context, ids []string) error {
		vectorCalls++
		require.Equal(t, []string{old.ID}, ids)
		return nil
	}))
	require.Equal(t, 2, vectorCalls)
	require.ErrorIs(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{other}, nil), errAttemptCleanupFailed)
	// Real SQL trigger failure must stop before vector deletion and remain visible.
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_sql_cleanup_failure'; END $$`).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_cleanup BEFORE UPDATE ON chunks FOR EACH ROW EXECUTE FUNCTION reject_cleanup()`).Error)
	require.ErrorIs(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{newer}, func(context.Context, []string) error { vectorCalls++; return nil }), errAttemptCleanupFailed)
	require.Equal(t, 2, vectorCalls)
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", newer.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
