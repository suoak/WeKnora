package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
)

// versionedSQLiteTables is the set of tables that SQLite migrations must
// create to stay in sync with the versioned (PostgreSQL) migrations:
// 000041 task queue, 000053 system settings, 000055 processing spans,
// 000063 knowledge multi-tags.
var versionedSQLiteTables = []string{
	"task_pending_ops",
	"task_dead_letters",
	"system_settings",
	"knowledge_processing_spans",
	"knowledge_tag_relations",
	"api_key_tenant_scopes",
	"api_key_kb_scopes",
	"tenant_portal_configs",
	"tenant_portal_stages",
	"tenant_access_requests",
	"model_usage_events",
	"mcp_usage_events",
	"usage_resource_links",
}

// versionedSQLiteColumns maps each existing table to the columns that the
// versioned migrations add and the SQLite baseline was missing.
var versionedSQLiteColumns = map[string][]string{
	"tenants":            {"api_principal_config"},                       // 000064
	"users":              {"is_system_admin"},                            // 000053
	"knowledges":         {"pending_subtasks_count"},                     // 000056
	"messages":           {"attachments", "usage"},                       // 000034, 000085
	"tenant_invitations": {"token", "accepted_count"},                    // 000054
	"embed_channels":     {"allow_memory"},                               // 000060
	"mcp_oauth_tokens":   {"principal_type", "principal_id"},             // 000064
	"tenant_api_keys":    {"owner_user_id", "client_type", "token_hint"}, // 000091, 003000
	"mcp_tool_approvals": {"enabled"},                                    // 000092
}

const expectedSQLiteMigrationVersion = 3000

func TestSQLiteMigrationsCreateVersionedSchema(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	chdirAndRestore(t, repoRoot)

	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db := openSQLiteDB(t, dbPath)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, version)
	require.False(t, dirty)

	for _, table := range versionedSQLiteTables {
		require.Truef(t, sqliteTableExists(t, db, table), "SQLite migrations must create table %s", table)
	}
	for table, columns := range versionedSQLiteColumns {
		for _, column := range columns {
			require.Truef(
				t,
				sqliteColumnExists(t, db, table, column),
				"SQLite migrations must add column %s.%s",
				table,
				column,
			)
		}
	}

	assertSQLiteShareLinkInvitationsWork(t, db)
	assertSQLiteMCPOAuthPrincipalUpsertWorks(t, db)
	require.False(t, sqliteColumnExists(t, db, "knowledges", "tag_id"),
		"SQLite migrations must drop legacy knowledges.tag_id after multi-tag migration")
}

func TestSQLiteMigrationsUpgradeV4PreservesData(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)

	// Build a legacy v4 migration root (000000_init .. 000004_memory) so we
	// can prove the new migrations upgrade an existing Lite database without
	// replaying the baseline.
	legacyRoot := copySQLiteMigrationsV4(t, repoRoot)
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "upgrade.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db := openSQLiteDB(t, dbPath)
	versionBefore, dirtyBefore := sqliteMigrationState(t, db)
	require.Equal(t, 4, versionBefore)
	require.False(t, dirtyBefore)
	_, err := db.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "upgrade-sentinel", "migration-test")
	require.NoError(t, err)
	_, err = db.Exec(
		"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, source, tag_id) "+
			"VALUES (?, 1, ?, 'document', 'tagged-doc', 'manual', ?)",
		"legacy-knowledge-1", "legacy-kb-1", "legacy-tag-1",
	)
	require.NoError(t, err)

	// Run the full migration set from the repo root.
	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db = openSQLiteDB(t, dbPath)
	versionAfter, dirtyAfter := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, versionAfter)
	require.False(t, dirtyAfter)

	for _, table := range versionedSQLiteTables {
		require.Truef(t, sqliteTableExists(t, db, table), "upgraded SQLite DB must have table %s", table)
	}
	for table, columns := range versionedSQLiteColumns {
		for _, column := range columns {
			require.Truef(
				t,
				sqliteColumnExists(t, db, table, column),
				"upgraded SQLite DB must have column %s.%s",
				table,
				column,
			)
		}
	}

	var sentinelName string
	require.NoError(t, db.QueryRow("SELECT name FROM tenants WHERE business = ?", "migration-test").Scan(&sentinelName))
	require.Equal(t, "upgrade-sentinel", sentinelName)

	var relationCount int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM knowledge_tag_relations WHERE knowledge_id = ? AND tag_id = ?",
		"legacy-knowledge-1", "legacy-tag-1",
	).Scan(&relationCount))
	require.Equal(t, 1, relationCount)
	require.False(t, sqliteColumnExists(t, db, "knowledges", "tag_id"))
}

func TestSQLiteMigrationsUpgradeV13ToLatest(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThroughV13(t, repoRoot)
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "upgrade-v13.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db := openSQLiteDB(t, dbPath)
	versionBefore, dirtyBefore := sqliteMigrationState(t, db)
	require.Equal(t, 13, versionBefore)
	require.False(t, dirtyBefore)
	require.True(t, sqliteColumnExists(t, db, "tenant_api_keys", "owner_user_id"))
	require.False(t, sqliteColumnExists(t, db, "mcp_tool_approvals", "enabled"))

	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	versionAfter, dirtyAfter := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, versionAfter)
	require.False(t, dirtyAfter)
	require.True(t, sqliteColumnExists(t, db, "tenant_api_keys", "owner_user_id"))
	require.True(t, sqliteColumnExists(t, db, "mcp_tool_approvals", "enabled"))

	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, migrator.Steps(-4))
	versionDowngraded, dirtyDowngraded := sqliteMigrationState(t, db)
	require.Equal(t, 14, versionDowngraded)
	require.False(t, dirtyDowngraded)
	require.True(t, sqliteColumnExists(t, db, "tenant_api_keys", "owner_user_id"))
	require.True(t, sqliteColumnExists(t, db, "mcp_tool_approvals", "enabled"))
	require.False(t, sqliteTableExists(t, db, "tenant_portal_configs"))
}

func TestSQLiteUserMCPCredentialLifecycleUpgradePreservesLegacySecret(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThrough(t, repoRoot, "000017")
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "user-mcp-v17.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	legacyHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_, err := db.Exec(`INSERT INTO tenant_api_keys
		(scope_type, owner_user_id, name, key_hash, api_key, full_access)
		VALUES ('user_mcp', 'legacy-owner', 'legacy-mcp', ?, 'synthetic-legacy-secret', 0)`, legacyHash)
	require.NoError(t, err)

	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, version)
	require.False(t, dirty)

	var clientType, tokenHint, apiKey, keyHash string
	require.NoError(t, db.QueryRow(`SELECT client_type, token_hint, api_key, key_hash
		FROM tenant_api_keys WHERE name='legacy-mcp'`).Scan(&clientType, &tokenHint, &apiKey, &keyHash))
	require.Equal(t, "generic", clientType)
	require.Empty(t, tokenHint)
	require.Equal(t, "synthetic-legacy-secret", apiKey, "DDL migration must not irreversibly clear legacy credentials")
	require.Equal(t, legacyHash, keyHash)
	_, err = db.Exec(`UPDATE tenant_api_keys SET client_type='future-client' WHERE name='legacy-mcp'`)
	require.NoError(t, err, "client_type must remain schema-extensible without an enum-style DB CHECK")
	var tableSQL string
	require.NoError(t, db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='tenant_api_keys'`).Scan(&tableSQL))
	require.NotContains(t, tableSQL, "workbuddy", "client allow-list belongs in application validation, not the DB schema")
}

func TestUserMCPCredentialLifecycleClientTypeHasNoDatabaseEnumCheck(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	for _, path := range []string{
		filepath.Join(repoRoot, "migrations", "versioned", "003000_user_mcp_credential_lifecycle.up.sql"),
		filepath.Join(repoRoot, "migrations", "sqlite", "003000_user_mcp_credential_lifecycle.up.sql"),
	} {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		sqlText := string(contents)
		require.Contains(t, sqlText, "client_type")
		require.NotContains(t, sqlText, "workbuddy", "client allow-list must not be encoded in migration SQL")
		require.NotContains(t, sqlText, "CHECK", "client_type must not require a migration for future client values")
	}
}

func TestSQLiteKnowledgePortalMigrationUpgradeConstraintsAndDown(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThrough(t, repoRoot, "000014")
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "portal-v14.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDBWithForeignKeys(t, dbPath)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 14, version)
	require.False(t, dirty)
	_, err := db.Exec("INSERT INTO tenants (id, name, business) VALUES (101, 'portal-space', 'migration-test')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO organizations (id, name, owner_id, owner_tenant_id) VALUES ('org-portal', 'Portal Org', 'owner', 101)")
	require.NoError(t, err)

	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, version)
	require.False(t, dirty)

	_, err = db.Exec(`INSERT INTO tenant_portal_configs
		(tenant_id, status, display_name, category, interaction_organization_id, created_by, updated_by)
		VALUES (101, 'published', 'Portal Space', 'hardware', 'org-portal', 'admin', 'admin')`)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE tenant_portal_configs SET category = 'architecture' WHERE tenant_id = 101")
	require.Error(t, err, "category must not reuse an IPD stage key")
	_, err = db.Exec("UPDATE tenant_portal_configs SET category = ' Hardware ' WHERE tenant_id = 101")
	require.Error(t, err, "category must be stored in canonical trim/lowercase form")
	_, err = db.Exec("INSERT INTO tenant_portal_stages (tenant_id, stage_key) VALUES (101, 'architecture')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO tenant_portal_stages (tenant_id, stage_key) VALUES (101, 'architecture')")
	require.Error(t, err, "a stage may occur only once per workspace")
	_, err = db.Exec("INSERT INTO tenant_portal_stages (tenant_id, stage_key) VALUES (101, 'all_process')")
	require.NoError(t, err, "the database must allow future catalog stages without a migration")

	insertRequest := `INSERT INTO tenant_access_requests
		(id, tenant_id, applicant_user_id, source, status, reason, requested_role)
		VALUES (?, 101, 'applicant', ?, ?, 'request access', ?)`
	_, err = db.Exec(insertRequest, "req-1", "portal", "pending", "viewer")
	require.NoError(t, err)
	_, err = db.Exec(insertRequest, "req-2", "portal", "pending", "viewer")
	require.Error(t, err, "only one pending request is allowed per workspace/applicant")
	_, err = db.Exec(insertRequest, "req-3", "portal", "rejected", "viewer")
	require.NoError(t, err, "historical non-pending requests remain allowed")
	_, err = db.Exec(insertRequest, "req-bad-source", "api", "rejected", "viewer")
	require.Error(t, err)
	_, err = db.Exec(insertRequest, "req-bad-role", "portal", "rejected", "admin")
	require.Error(t, err)

	_, err = db.Exec("DELETE FROM organizations WHERE id = 'org-portal'")
	require.NoError(t, err)
	var orgID sql.NullString
	require.NoError(t, db.QueryRow("SELECT interaction_organization_id FROM tenant_portal_configs WHERE tenant_id = 101").Scan(&orgID))
	require.False(t, orgID.Valid, "organization hard delete must SET NULL")

	_, err = db.Exec("DELETE FROM tenants WHERE id = 101")
	require.NoError(t, err)
	for _, table := range []string{"tenant_portal_configs", "tenant_portal_stages", "tenant_access_requests"} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Zero(t, count, "%s must cascade on tenant hard delete", table)
	}

	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, migrator.Steps(-4))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 14, version)
	require.False(t, dirty)
	for _, table := range []string{"tenant_portal_configs", "tenant_portal_stages", "tenant_access_requests"} {
		require.False(t, sqliteTableExists(t, db, table))
	}
}

func TestSQLiteUsageAnalyticsMigrationUpgradeV15AndDown(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThrough(t, repoRoot, "000015")
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "usage-v15.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 15, version)
	require.False(t, dirty)

	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, version)
	require.False(t, dirty)
	for _, table := range []string{"model_usage_events", "mcp_usage_events", "usage_resource_links"} {
		require.True(t, sqliteTableExists(t, db, table), table)
	}

	_, err := db.Exec(`INSERT INTO model_usage_events
		(event_key, tenant_id, principal_type, channel, operation, input_tokens, output_tokens, total_tokens,
		 cache_read_tokens, cache_write_tokens, reasoning_tokens, usage_source, status, occurred_at)
		VALUES ('message:kept-after-delete', 999, 'web_user', 'web', 'knowledge_qa_turn', 10, 5, 15, 4, 0, 0, 'provider', 'completed', CURRENT_TIMESTAMP)`)
	require.NoError(t, err, "analytics ledger must not require a live tenant or message foreign key")
	_, err = db.Exec(`INSERT INTO model_usage_events
		(event_key, tenant_id, principal_type, channel, operation, input_tokens, output_tokens, total_tokens,
		 cache_read_tokens, cache_write_tokens, reasoning_tokens, usage_source, status, occurred_at)
		VALUES ('message:kept-after-delete', 999, 'web_user', 'web', 'knowledge_qa_turn', 10, 5, 15, 4, 0, 0, 'provider', 'completed', CURRENT_TIMESTAMP)`)
	require.Error(t, err, "event_key must make retries idempotent")

	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, migrator.Steps(-3))
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 15, version)
	require.False(t, dirty)
	for _, table := range []string{"model_usage_events", "mcp_usage_events", "usage_resource_links"} {
		require.False(t, sqliteTableExists(t, db, table), table)
	}
}

func sqliteRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return repoRoot
}

func chdirAndRestore(t *testing.T, dir string) {
	t.Helper()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
}

func openSQLiteDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openSQLiteDBWithForeignKeys(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sqliteMigrationState(t *testing.T, db *sql.DB) (version int, dirty bool) {
	t.Helper()
	require.NoError(t, db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	return version, dirty
}

func sqliteTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		table,
	).Scan(&n))
	return n == 1
}

func sqliteColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
		table,
		column,
	).Scan(&n))
	return n == 1
}

func assertSQLiteShareLinkInvitationsWork(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "share-link-tenant", "share-link-test")
	require.NoError(t, err)

	expiresAt := "2099-01-01 00:00:00"
	shareLinkInsert := "INSERT INTO tenant_invitations " +
		"(tenant_id, invitee_user_id, token, role, status, expires_at) " +
		"VALUES (1, '', ?, 'member', 'pending', ?)"
	_, err = db.Exec(shareLinkInsert, "token-a", expiresAt)
	require.NoError(t, err)
	_, err = db.Exec(shareLinkInsert, "token-b", expiresAt)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM tenant_invitations WHERE tenant_id = 1 AND invitee_user_id = '' AND status = 'pending'",
	).Scan(&count))
	require.Equal(t, 2, count)
}

func assertSQLiteMCPOAuthPrincipalUpsertWorks(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO mcp_services (id, tenant_id, name, transport_type) VALUES (?, 1, 'svc', 'http')",
		"svc-migration-1",
	)
	require.NoError(t, err)

	tokenInsertPrefix := "INSERT INTO mcp_oauth_tokens " +
		"(id, tenant_id, user_id, service_id, principal_type, principal_id, access_token) "
	_, err = db.Exec(
		tokenInsertPrefix +
			"VALUES ('tok-1', 1, 'u1', 'svc-migration-1', 'web_user', 'u1', 'token-1')",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		tokenInsertPrefix +
			"VALUES ('tok-2', 1, 'u1', 'svc-migration-1', 'web_user', 'u1', 'token-2') " +
			"ON CONFLICT(tenant_id, principal_type, principal_id, service_id) " +
			"DO UPDATE SET access_token = excluded.access_token",
	)
	require.NoError(t, err)

	var accessToken string
	require.NoError(t, db.QueryRow(
		"SELECT access_token FROM mcp_oauth_tokens "+
			"WHERE tenant_id = 1 AND principal_type = 'web_user' "+
			"AND principal_id = 'u1' AND service_id = 'svc-migration-1'",
	).Scan(&accessToken))
	require.Equal(t, "token-2", accessToken)

	var rowCount int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM mcp_oauth_tokens WHERE tenant_id = 1 AND service_id = 'svc-migration-1'",
	).Scan(&rowCount))
	require.Equal(t, 1, rowCount)
}

func copySQLiteMigrationsV4(t *testing.T, repoRoot string) string {
	t.Helper()
	dest := t.TempDir()
	srcDir := filepath.Join(repoRoot, "migrations", "sqlite")
	destDir := filepath.Join(dest, "migrations", "sqlite")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	legacy := []string{
		"000000_init.up.sql",
		"000001_remove_wiki_log.up.sql",
		"000002_knowledge_folder_path.up.sql",
		"000003_knowledge_base_auto_tag_config.up.sql",
		"000004_memory.up.sql",
	}
	for _, name := range legacy {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(destDir, name), data, 0o600))
	}
	return dest
}

func copySQLiteMigrationsThroughV13(t *testing.T, repoRoot string) string {
	return copySQLiteMigrationsThrough(t, repoRoot, "000013")
}

func copySQLiteMigrationsThrough(t *testing.T, repoRoot, latest string) string {
	t.Helper()
	dest := t.TempDir()
	srcDir := filepath.Join(repoRoot, "migrations", "sqlite")
	destDir := filepath.Join(dest, "migrations", "sqlite")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 6 || name[:6] > latest {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(destDir, name), data, 0o600))
	}
	return dest
}
