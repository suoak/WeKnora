package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
)

// Exercise the actual project runner, not just a hand-built policy fixture.
func TestSQLiteEnterprisePublicationRealRunnerUpgradeDownUp(t *testing.T) {
	root := sqliteRepoRoot(t)
	chdirAndRestore(t, root)
	path := filepath.Join(t.TempDir(), "enterprise-legacy.db")
	db := openSQLiteDB(t, path)
	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(3033)) // Actual current source; no Force or composed directory.
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 3033, version)
	require.False(t, dirty)
	// FK enforcement is enabled for the new policies. The legacy sentinel is
	// an existing machine credential, never a production or usable secret.
	_, err = db.Exec(`INSERT INTO tenants(id,name,business) VALUES(99,'synthetic','m2')`)
	require.NoError(t, err)
	for _, query := range []string{
		`INSERT INTO users(id,username,email,password_hash,tenant_id) VALUES('b4-user` +
			`','synthetic','b4@example.invalid','synthetic-not-password',99)`,
		`INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('b4-user',99,'viewer','active')`,
		`INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,u` +
			`pdated_by) VALUES(99,'published','synthetic','b4-user','b4-user')`,
		`INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_mod` +
			`el_id) VALUES('b4-kb','synthetic',99,'synthetic','synthetic')`,
		`INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source) VAL` +
			`UES('b4-doc',99,'b4-kb','file','synthetic historical','synthetic local sourc` +
			`e')`,
	} {
		_, err = db.Exec(query)
		require.NoError(t, err)
	}
	_, err = db.Exec(
		`INSERT INTO tenant_api_keys(tenant_id,scope_type,name,key_hash,api_key,full_` +
			`access,capabilities) VALUES(99,'tenant','m2-old-key','synthetic-m2-hash','sy` +
			`nthetic-never-a-key',0,'["retrieve"]')`,
	)
	require.NoError(t, err)
	tables := []string{
		"tenants",
		"users",
		"tenant_members",
		"tenant_api_keys",
		"tenant_portal_configs",
		"knowledge_bases",
		"knowledges",
	}
	snapshots := map[string][32]byte{}
	for _, table := range tables {
		snapshots[table] = sqliteEnterpriseRowsDigest(t, db, table)
	}
	chdirAndRestore(t, root)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: path}))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 3040, version)
	require.False(t, dirty)
	for _, table := range []string{
		"enterprise_space_policies",
		"kb_publication_policies",
		"knowledge_access_policies",
		"document_subject_grants",
	} {
		var count int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, "historical resources cannot be backfilled as approved")
	}
	var caps, secret string
	require.NoError(
		t,
		db.QueryRow(`SELECT capabilities,api_key FROM tenant_api_keys WHERE name='m2-old-key'`).
			Scan(&caps, &secret),
	)
	require.Equal(t, `["retrieve"]`, caps)
	require.Equal(t, "synthetic-never-a-key", secret)
	for _, table := range tables {
		require.Equal(t, snapshots[table], sqliteEnterpriseRowsDigest(t, db, table), table)
	}
	require.NoError(t, m.Migrate(3033))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 3033, version)
	require.False(t, dirty)
	for _, table := range []string{
		"enterprise_space_policies",
		"kb_publication_policies",
		"knowledge_access_policies",
		"document_subject_grants",
		"document_access_epochs",
	} {
		require.False(t, sqliteTableExists(t, db, table))
	}
	require.NoError(t, m.Up())
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 3040, version)
	require.False(t, dirty)
	require.NoError(
		t,
		db.QueryRow(`SELECT capabilities,api_key FROM tenant_api_keys WHERE name='m2-old-key'`).
			Scan(&caps, &secret),
	)
	require.Equal(t, `["retrieve"]`, caps)
	require.Equal(t, "synthetic-never-a-key", secret)
	for _, table := range tables {
		require.Equal(t, snapshots[table], sqliteEnterpriseRowsDigest(t, db, table), table)
	}
}

func sqliteEnterpriseRowsDigest(t *testing.T, db *sql.DB, table string) [32]byte {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " ORDER BY rowid")
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	all := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		require.NoError(t, rows.Scan(pointers...))
		original := make([]any, 0, len(values))
		for i, column := range columns {
			if table == "tenant_api_keys" && column == "personal_scope_mode" {
				// The only new credential metadata must remain the conservative
				// fixed default. Compare every pre-existing column unchanged.
				value := values[i]
				if bytes, ok := value.([]byte); ok {
					value = string(bytes)
				}
				require.Equal(t, "fixed", value)
				continue
			}
			original = append(original, values[i])
		}
		all = append(all, original)
	}
	require.NoError(t, rows.Err())
	body, err := json.Marshal(all)
	require.NoError(t, err)
	return sha256.Sum256(body)
}

func TestSQLiteEnterpriseMigrationFailureAndSnapshotRecovery(t *testing.T) {
	root := sqliteRepoRoot(t)
	chdirAndRestore(t, root)
	path := filepath.Join(t.TempDir(), "failure.db")
	db := openSQLiteDB(t, path)
	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(3033))
	backup := filepath.Join(t.TempDir(), "backup.db")
	_, err = db.Exec("VACUUM INTO ?", backup)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE enterprise_space_policies(conflicting_shape TEXT)")
	require.NoError(t, err)
	err = RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: path})
	require.Error(t, err)
	require.Contains(t, err.Error(), "3034")
	version, dirty, ok := CachedMigrationVersion()
	require.True(t, ok)
	require.Equal(t, uint(3034), version)
	require.True(t, dirty)
	require.NotEmpty(t, CachedMigrationError())
	require.Error(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: path}))
	// Restore a verified pre-upgrade snapshot to a NEW isolated path, retaining
	// the failed DB for diagnosis. Never Force, edit version rows or auto-recover.
	body, err := os.ReadFile(backup)
	require.NoError(t, err)
	recovered := filepath.Join(t.TempDir(), "restored.db")
	require.NoError(t, os.WriteFile(recovered, body, 0o600))
	require.NoError(
		t,
		RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: recovered}),
	)
	version, dirty, ok = CachedMigrationVersion()
	require.True(t, ok)
	require.Equal(t, uint(3040), version)
	require.False(t, dirty)
	require.Empty(t, CachedMigrationError())
}
