package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceDefaultsMigrationUpDownPreservesResourcePins(t *testing.T) {
	for _, dialect := range []string{"sqlite", "versioned"} {
		t.Run(dialect, func(t *testing.T) {
			driver, dsn := "sqlite3", filepath.Join(t.TempDir(), "defaults.db")
			if dialect == "versioned" {
				driver = "postgres"
				dsn = os.Getenv("WEKNORA_MODEL_DEFAULTS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("requires isolated PostgreSQL acceptance database")
				}
			}
			db, err := sql.Open(driver, dsn)
			require.NoError(t, err)
			defer db.Close()
			ctx := context.Background()
			conn, err := db.Conn(ctx)
			require.NoError(t, err)
			defer conn.Close()
			if dialect == "versioned" {
				schema := "model_defaults_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				_, err = conn.ExecContext(ctx, "CREATE SCHEMA "+schema)
				require.NoError(t, err)
				defer func() { _, _ = conn.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
				_, err = conn.ExecContext(ctx, "SET search_path TO "+schema)
				require.NoError(t, err)
			}
			for _, statement := range []string{
				"CREATE TABLE tenants (id BIGINT PRIMARY KEY, name TEXT)",
				"CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, embedding_model_id TEXT)",
				"INSERT INTO tenants VALUES (7, 'existing-workspace')",
				"INSERT INTO knowledge_bases VALUES ('existing-kb', 'pinned-embedding-v1')",
			} {
				_, err = conn.ExecContext(ctx, statement)
				require.NoError(t, err)
			}
			apply := func(direction string) {
				data, readErr := os.ReadFile(filepath.Join(sqliteRepoRoot(t), "migrations", dialect, "003049_workspace_model_defaults."+direction+".sql"))
				require.NoError(t, readErr)
				_, execErr := conn.ExecContext(ctx, string(data))
				require.NoError(t, execErr)
			}
			apply("up")
			var defaults sql.NullString
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT default_settings FROM tenants WHERE id=7").Scan(&defaults))
			require.False(t, defaults.Valid)
			_, err = conn.ExecContext(ctx, `UPDATE tenants SET default_settings='{"chat_model_id":"new-default"}' WHERE id=7`)
			require.NoError(t, err)
			apply("down")
			apply("up")
			var pin, name string
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT embedding_model_id FROM knowledge_bases WHERE id='existing-kb'").Scan(&pin))
			require.Equal(t, "pinned-embedding-v1", pin)
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT name FROM tenants WHERE id=7").Scan(&name))
			require.Equal(t, "existing-workspace", name)
		})
	}
}
