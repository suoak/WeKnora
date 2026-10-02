// Package syncdb creates isolated, disposable databases for sync execution tests.
// It never uses the application's production database configuration.
package syncdb

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if dialect == "sqlite" {
		dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "sync.db")) + "?_busy_timeout=10000&_journal_mode=WAL"
		db, err = gorm.Open(sqlite.Open(dsn), config)
	} else {
		dsn := os.Getenv("SYNC_EXECUTION_POSTGRES_DSN")
		if dsn == "" {
			if os.Getenv("REQUIRE_SYNC_EXECUTION_POSTGRES") == "1" {
				t.Fatal("real PostgreSQL DSN required")
			}
			t.Skip("SYNC_EXECUTION_POSTGRES_DSN is not configured")
		}
		// Test credentials must target the disposable Actions service, never
		// DATABASE_URL or any production configuration fallback.
		admin, openErr := gorm.Open(postgres.Open(dsn), config)
		require.NoError(t, openErr)
		adminPool, poolErr := admin.DB()
		require.NoError(t, poolErr)
		schema := "sync_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)
		require.Contains(t, parsed.Scheme, "postgres")
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		db, err = gorm.Open(postgres.Open(parsed.String()), config)
		t.Cleanup(func() {
			// Exact schema is generated above solely for this test; no public
			// schema or user-provided identifier is ever dropped.
			require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
			_ = adminPool.Close()
		})
	}
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(24)
	pool.SetMaxIdleConns(24)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
	return db
}
