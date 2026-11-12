package database

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only the already-deployed production lineage substitutes the colliding 3046.
// All other files are the official chain, including the forward reconciliation.
func reconciliationMigrationSource(t *testing.T, dialect, lineage string) string {
	t.Helper()
	root := sqliteRepoRoot(t)
	dir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(root, "migrations", dialect))
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if lineage == "production" && strings.HasPrefix(entry.Name(), "003046_") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, "migrations", dialect, entry.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, entry.Name()), body, 0o600))
	}
	if lineage == "production" {
		body := []byte("SELECT 1;\n")
		if dialect == "versioned" {
			body, err = os.ReadFile(
				filepath.Join(
					root,
					"internal",
					"database",
					"testdata",
					"production_3046_feishu_local_deletion.up.sql",
				),
			)
			require.NoError(t, err)
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "003046_production_feishu.up.sql"), body, 0o600))
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join(dir, "003046_production_feishu.down.sql"),
				[]byte("SELECT 1;\n"),
				0o600,
			),
		)
	}
	return "file://" + filepath.ToSlash(dir)
}

func TestSQLite3046LineagesReconcileWithoutLosingUsage(t *testing.T) {
	for _, lineage := range []string{"main", "production"} {
		t.Run(lineage, func(t *testing.T) {
			db := openSQLiteDB(t, filepath.Join(t.TempDir(), "lineage.db"))
			driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
			require.NoError(t, err)
			m, err := migrate.NewWithDatabaseInstance(
				reconciliationMigrationSource(t, "sqlite", lineage),
				"sqlite3",
				driver,
			)
			require.NoError(t, err)
			require.NoError(t, m.Migrate(3046))
			_, err = db.Exec(`INSERT INTO mcp_usage_events
			 (id,event_key,principal_id,direction,tool_name,success,occurred_at)
			 VALUES(17,'retained','synthetic-user','inbound','search_knowledge',1,CURRENT_TIMESTAMP)`)
			require.NoError(t, err)
			if lineage == "main" {
				_, err = db.Exec(
					"UPDATE mcp_usage_events SET entry_point='go_personal'," +
						"endpoint_id='personal',client_source='negotiated'",
				)
				require.NoError(t, err)
			}
			_, err = db.Exec(
				`INSERT INTO usage_resource_links(id,event_kind,event_id,resource_type,resource_id,
					resource_tenant_id)
			 VALUES(23,'mcp',17,'knowledge_base','synthetic-kb',99);
			 UPDATE sqlite_sequence SET seq=1000 WHERE name IN ('mcp_usage_events','usage_resource_links')`,
			)
			require.NoError(t, err)
			require.NoError(t, m.Up())
			version, dirty := sqliteMigrationState(t, db)
			require.Equal(t, 3047, version)
			require.False(t, dirty)
			var entry, client, principal string
			var endpoint sql.NullString
			require.NoError(
				t,
				db.QueryRow("SELECT entry_point,endpoint_id,client_source,principal_id FROM "+
					"mcp_usage_events WHERE id=17").
					Scan(&entry, &endpoint, &client, &principal),
			)
			require.Equal(t, "synthetic-user", principal)
			if lineage == "main" {
				require.Equal(t, "go_personal", entry)
				require.Equal(t, "personal", endpoint.String)
				require.Equal(t, "negotiated", client)
			} else {
				require.Equal(t, "legacy", entry)
				require.False(t, endpoint.Valid)
				require.Equal(t, "unknown", client)
			}
			var eventID int
			require.NoError(
				t,
				db.QueryRow(`SELECT event_id FROM usage_resource_links WHERE id=23`).Scan(&eventID),
			)
			require.Equal(t, 17, eventID)
			for _, table := range []string{"mcp_usage_events", "usage_resource_links"} {
				var sequence int
				require.NoError(
					t,
					db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name=?`, table).Scan(&sequence),
				)
				require.Equal(t, 1000, sequence)
			}
			_, err = db.Exec(
				`INSERT INTO usage_resource_links(event_kind,event_id,resource_type,resource_id,resource_tenant_id)
			 VALUES('mcp',17,'knowledge_base_hit','synthetic-kb',99)`,
			)
			require.NoError(t, err)
			// A one-step rollback retains both additive branch features; up is repeatable.
			require.NoError(t, m.Migrate(3046))
			require.NoError(t, m.Up())
			require.NoError(
				t,
				db.QueryRow(`SELECT event_id FROM usage_resource_links WHERE id=23`).Scan(&eventID),
			)
			require.Equal(t, 17, eventID)
		})
	}
}

func TestPostgres3046LineagesReconcileWithoutLosingUsage(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	for _, lineage := range []string{"main", "production"} {
		t.Run(lineage, func(t *testing.T) {
			name := "reconcile_3046_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			_, err := admin.Exec("CREATE DATABASE " + name)
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
			u, err := url.Parse(dsn)
			require.NoError(t, err)
			u.Path = "/" + name
			db, err := sql.Open("postgres", u.String())
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			m, err := migrate.New(reconciliationMigrationSource(t, "versioned", lineage), u.String())
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(3046))
			_, err = db.Exec(
				`INSERT INTO mcp_usage_events(event_key,principal_id,direction,tool_name,success,occurred_at)
			 VALUES('retained','synthetic-user','inbound','search_knowledge',true,CURRENT_TIMESTAMP)`,
			)
			require.NoError(t, err)
			if lineage == "main" {
				_, err = db.Exec(
					`UPDATE mcp_usage_events SET entry_point='go_personal',endpoint_id='personal',
						client_source='negotiated';
				 INSERT INTO mcp_usage_metadata_rollback VALUES('retained','stale','stale','unknown')`,
				)
				require.NoError(t, err)
			}
			require.NoError(t, m.Up())
			version, dirty, err := m.Version()
			require.NoError(t, err)
			require.EqualValues(t, 3047, version)
			require.False(t, dirty)
			var entry, principal, constraint, function string
			require.NoError(
				t,
				db.QueryRow(`SELECT entry_point,principal_id FROM mcp_usage_events WHERE event_key='retained'`).
					Scan(&entry, &principal),
			)
			require.Equal(t, "synthetic-user", principal)
			if lineage == "main" {
				require.Equal(
					t,
					"go_personal",
					entry,
					"stale rollback metadata must not overwrite current statistics",
				)
			} else {
				require.Equal(t, "legacy", entry)
			}
			require.NoError(
				t,
				db.QueryRow("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE "+
					"conname='feishu_legacy_resources_state_check'").
					Scan(&constraint),
			)
			require.Contains(t, constraint, "deleted")
			require.NoError(
				t,
				db.QueryRow(`SELECT pg_get_functiondef('knowhub_feishu_legacy_write_fence()'::regprocedure)`).
					Scan(&function),
			)
			require.Contains(t, function, "deleted Feishu document cannot be republished")
			require.NoError(t, m.Migrate(3046))
			require.NoError(t, m.Up())
			require.NoError(
				t,
				db.QueryRow(`SELECT entry_point FROM mcp_usage_events WHERE event_key='retained'`).
					Scan(&entry),
			)
			if lineage == "main" {
				require.Equal(t, "go_personal", entry)
			}
		})
	}
}
