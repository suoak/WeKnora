//go:build knowhub_m3_postgres

package repository

import (
	"net/url"
	"os"
	"strings"
	"testing"

	projectdb "github.com/Tencent/WeKnora/internal/database"

	"github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Uses the RC3-aligned candidate's complete official source in a disposable
// database. No environment skip: missing/wrong test DSN fails explicitly.
func TestDocumentM3PostgresRealMigrationRunner(t *testing.T) {
	runDocumentPostgresMigration(t, "knowhub_m3_chain", "file://../../../migrations/versioned")
}

// A second initially empty database exercises the same complete candidate source.
func TestDocumentM3PostgresFreshMigrationRunner(t *testing.T) {
	runDocumentPostgresMigration(t, "knowhub_m3_fresh", "file://../../../migrations/versioned")
}

func TestDocumentM3PostgresEmptyStartupMigrationRunner(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M2_TEST_POSTGRES_DSN")
	require.Regexp(t, `^host=/home/aqyfzx/ai/knowhub-mcp-m4-[0-9]{8}-[a-f0-9]{6}/pgsocket-[0-9]+ user=postgres dbname=knowhub_m2_test sslmode=disable$`, dsn)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir("../../.."))
	socket := strings.Fields(dsn)[0][len("host="):]
	migrationURL := "postgresql://postgres@/knowhub_m3_empty?host=" + url.QueryEscape(socket) + "&sslmode=disable"
	migrationErr := projectdb.RunMigrationsWithOptions(migrationURL, projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, migrationErr)
	version, dirty, ok := projectdb.CachedMigrationVersion()
	require.True(t, ok)
	require.Equal(t, uint(3040), version)
	require.False(t, dirty)
	require.Empty(t, projectdb.CachedMigrationError())
}

func TestDocumentM3PostgresFailureRecoveryMigrationRunner(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M2_TEST_POSTGRES_DSN")
	require.Regexp(t, `^host=/home/aqyfzx/ai/knowhub-mcp-m4-[0-9]{8}-[a-f0-9]{6}/pgsocket-[0-9]+ user=postgres dbname=knowhub_m2_test sslmode=disable$`, dsn)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	adminDB, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adminDB.Close() })
	socket := strings.Fields(dsn)[0][len("host="):]
	databaseURL := func(name string) string {
		return "postgresql://postgres@/" + name + "?host=" + url.QueryEscape(socket) + "&sslmode=disable"
	}
	m, err := migrate.New("file://../../../migrations/versioned", databaseURL("knowhub_m3_failure"))
	require.NoError(t, err)
	require.NoError(t, m.Migrate(3033))
	sourceErr, databaseErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, databaseErr)
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_m3_backup TEMPLATE knowhub_m3_failure").Error)
	db, err := gorm.Open(postgres.Open(strings.Replace(dsn, "dbname=knowhub_m2_test", "dbname=knowhub_m3_failure", 1)), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.Exec("CREATE TABLE enterprise_space_policies(conflicting_shape TEXT)").Error)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir("../../.."))
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	for attempt := 0; attempt < 2; attempt++ {
		migrationErr := projectdb.RunMigrationsWithOptions(databaseURL("knowhub_m3_failure"), projectdb.MigrationOptions{})
		require.Error(t, migrationErr)
		require.Contains(t, migrationErr.Error(), "3034")
		version, dirty, ok := projectdb.CachedMigrationVersion()
		require.True(t, ok)
		require.Equal(t, uint(3034), version)
		require.True(t, dirty)
		require.NotEmpty(t, projectdb.CachedMigrationError())
	}
	// Restore into a new isolated database, preserving the failed DB. No Force
	// or changes to migration/version records are used for recovery.
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_m3_restored TEMPLATE knowhub_m3_backup").Error)
	require.NoError(t, projectdb.RunMigrationsWithOptions(databaseURL("knowhub_m3_restored"), projectdb.MigrationOptions{}))
	version, dirty, ok := projectdb.CachedMigrationVersion()
	require.True(t, ok)
	require.Equal(t, uint(3040), version)
	require.False(t, dirty)
	require.Empty(t, projectdb.CachedMigrationError())
	require.NoError(t, os.Chdir(cwd))
}

func runDocumentPostgresMigration(t *testing.T, databaseName, source string) {
	t.Helper()
	dsn := os.Getenv("KNOWHUB_M2_TEST_POSTGRES_DSN")
	require.Contains(t, dsn, "dbname=knowhub_m2_test")
	require.Regexp(t, `host=/home/aqyfzx/ai/knowhub-mcp-m[34]-[0-9]{8}-[a-f0-9]{6}/pgsocket-[0-9]+ `, dsn)
	dsn = strings.Replace(dsn, "dbname=knowhub_m2_test", "dbname="+databaseName, 1)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	driver, err := pgmigrate.WithInstance(conn, &pgmigrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance(source, "postgres", driver)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	// Establish the recorded 3033 state by executing this candidate's complete
	// official history. No Force, empty migration or composed source directory.
	require.NoError(t, m.Migrate(3033))
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, uint(3033), version)
	require.False(t, dirty)
	for _, q := range []string{
		`INSERT INTO tenants(id,name,business) VALUES(990099,'synthetic M3','isolated')`,
		`INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES('m3-historical-kb','synthetic',990099,'synthetic','synthetic')`,
		`INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source) VALUES('m3-historical-doc',990099,'m3-historical-kb','file','synthetic historical','synthetic local source')`,
		`INSERT INTO users(id,username,email,password_hash,tenant_id) VALUES('b4-user','synthetic','b4@example.invalid','synthetic-not-password',990099)`,
		`INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('b4-user',990099,'viewer','active')`,
		`INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by) VALUES(990099,'published','synthetic','b4-user','b4-user')`,
		`INSERT INTO tenant_api_keys(tenant_id,scope_type,name,key_hash,api_key,full_access,capabilities) VALUES(990099,'tenant','m3-old','synthetic-m3-not-a-key-hash','synthetic-never-a-key',false,'["retrieve"]')`,
	} {
		require.NoError(t, db.Exec(q).Error)
	}
	var before string
	signature := `SELECT md5(string_agg(table_name||':'||column_name||':'||data_type,',' ORDER BY table_name,column_name)) FROM information_schema.columns WHERE table_schema='public' AND table_name NOT IN ('enterprise_space_policies','kb_publication_policies','knowledge_access_policies','document_subject_grants','document_access_epochs','feishu_legacy_resources','feishu_legacy_attempts','feishu_legacy_index_candidates','wiki_mcp_provenance','wiki_mcp_kb_epochs') AND NOT (table_name='data_sources' AND column_name='legacy_access_state') AND NOT (table_name='tenant_api_keys' AND column_name='personal_scope_mode')`
	require.NoError(t, db.Raw(signature).Scan(&before).Error)
	tables := []string{"tenants", "users", "tenant_members", "tenant_api_keys", "tenant_portal_configs", "knowledge_bases", "knowledges"}
	snapshots := map[string]string{}
	originalRows := func(table string) string {
		projection := "to_jsonb(t)"
		if table == "tenant_api_keys" {
			projection = "(to_jsonb(t)-'personal_scope_mode')"
		}
		var rows string
		require.NoError(t, db.Raw("SELECT COALESCE(jsonb_agg("+projection+" ORDER BY "+projection+"::text)::text,'[]') FROM "+table+" t").Scan(&rows).Error)
		return rows
	}
	for _, table := range tables {
		snapshots[table] = originalRows(table)
	}
	var firstIncarnation string
	for attempt := 0; attempt < 2; attempt++ {
		// Also exercise the actual application startup migration entry point.
		cwd, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir("../../.."))
		socket := strings.Fields(dsn)[0][len("host="):]
		migrationURL := "postgresql://postgres@/" + databaseName + "?host=" + url.QueryEscape(socket) + "&sslmode=disable"
		migrationErr := projectdb.RunMigrationsWithOptions(migrationURL, projectdb.MigrationOptions{})
		require.NoError(t, os.Chdir(cwd))
		require.NoError(t, migrationErr)
		version, dirty, err := m.Version()
		require.NoError(t, err)
		require.Equal(t, uint(3040), version)
		require.False(t, dirty)
		var after string
		require.NoError(t, db.Raw(signature).Scan(&after).Error)
		require.Equal(t, before, after)
		for _, table := range tables {
			require.Equal(t, snapshots[table], originalRows(table), "every original column remains unchanged: "+table)
		}
		var mode string
		require.NoError(t, db.Raw("SELECT personal_scope_mode FROM tenant_api_keys WHERE name='m3-old'").Scan(&mode).Error)
		require.Equal(t, "fixed", mode, "the sole added Key field must not expand historical authority")

		for _, table := range []string{"enterprise_space_policies", "kb_publication_policies", "knowledge_access_policies", "document_subject_grants", "wiki_mcp_provenance"} {
			var count int64
			require.NoError(t, db.Table(table).Count(&count).Error)
			require.Zero(t, count, "no automatic historical approval")
		}
		var key struct {
			Capabilities string
			FullAccess   bool
		}
		require.NoError(t, db.Table("tenant_api_keys").Select("capabilities,full_access").Where("name='m3-old'").Take(&key).Error)
		require.Equal(t, `["retrieve"]`, key.Capabilities)
		require.False(t, key.FullAccess)
		var incarnation string
		require.NoError(t, db.Table("document_access_epochs").Select("incarnation").Where("knowledge_id='m3-historical-doc'").Scan(&incarnation).Error)
		require.Len(t, incarnation, 32)
		if attempt == 0 {
			firstIncarnation = incarnation
			require.NoError(t, m.Migrate(3034))
			version, dirty, err = m.Version()
			require.NoError(t, err)
			require.Equal(t, uint(3034), version)
			require.False(t, dirty)
		} else {
			require.NotEqual(t, firstIncarnation, incarnation)
		}
	}
	// The approved rollback boundary is the confirmed RC3 version. Never run
	// destructive historical down migrations below 3033 for this acceptance.
	require.NoError(t, m.Migrate(3033))
	version, dirty, err = m.Version()
	require.NoError(t, err)
	require.Equal(t, uint(3033), version)
	require.False(t, dirty)
	require.NoError(t, m.Up())
	for _, table := range tables {
		require.Equal(t, snapshots[table], originalRows(table), "legacy rows unchanged after fixture-only 3033 rollback/up: "+table)
	}
}
