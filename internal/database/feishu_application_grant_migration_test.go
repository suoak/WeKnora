package database

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplicationGrantFreshAndOffline3031Upgrade(t *testing.T) {
	root := sqliteRepoRoot(t)
	chdirAndRestore(t, root)
	for _, kind := range []string{"FRESH", "CLONE"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("FEISHU_APPLICATION_MIGRATION_" + kind + "_DSN")
			if dsn == "" {
				t.Skip("isolated application migration DSN not configured")
			}
			require.Contains(t, dsn, "@knowhub-p1-dispatch-pg-20261018:5432/application_p2_")
			db, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			defer func(cleanup func() error) { _ = cleanup() }(db.Close)
			var version int
			var dirty bool
			if kind == "CLONE" {
				require.NoError(
					t,
					db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty),
				)
				require.Equal(t, 3031, version)
				require.False(t, dirty)
			}
			require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))
			require.NoError(
				t,
				db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty),
			)
			require.Equal(t, 3033, version)
			require.False(t, dirty)
			require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))
			m, err := migrate.New("file://migrations/versioned", dsn)
			require.NoError(t, err)
			defer func(cleanup func() (error, error)) { _, _ = cleanup() }(m.Close)
			require.NoError(t, m.Steps(-1))
			require.NoError(
				t,
				db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty),
			)
			require.Equal(t, 3031, version)
			require.False(t, dirty)
			require.NoError(t, m.Steps(1))
			require.NoError(
				t,
				db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty),
			)
			require.Equal(t, 3033, version)
			require.False(t, dirty)
		})
	}
}

func TestApplicationGrantMigrationGuards(t *testing.T) {
	for _, dialect := range []string{"postgres", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			require.NoError(t, db.Exec("CREATE TABLE users(id varchar(36) PRIMARY KEY)").Error)
			require.NoError(t, db.Exec("CREATE TABLE knowledge_bases(id varchar(36) PRIMARY KEY)").Error)
			dir := "versioned"
			if dialect == "sqlite" {
				dir = "sqlite"
			}
			root := sqliteRepoRoot(t)
			ddl := func(name string) string {
				b, e := os.ReadFile(root + "/migrations/" + dir + "/" + name + ".sql")
				require.NoError(t, e)
				return string(b)
			}
			require.NoError(t, db.Exec(ddl("003025_feishu_personal.up")).Error)
			require.NoError(
				t,
				db.Exec("INSERT INTO users VALUES('A'),('B'); INSERT INTO knowledge_bases "+
					"VALUES('kb'),('kb2')").Error,
			)
			require.NoError(
				t,
				db.Exec(
					"INSERT INTO feishu_personal_imports(id,user_id,app_id,open_id,kb_id,"+
						"document_token,resource_type,url,knowledge_id) VALUES('legacy','A',"+
						"'cli_test','real-user','kb','old','docx','https://test.invalid','K1')",
				).Error,
			)
			require.NoError(t, db.Exec(ddl("003031_feishu_personal_recovery.up")).Error)
			up, down := ddl(
				"003033_feishu_application_grant.up",
			), ddl(
				"003033_feishu_application_grant.down",
			)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(up).Error }))
			var mode, kid string
			require.NoError(
				t,
				db.Raw("SELECT auth_mode FROM feishu_personal_imports WHERE id='legacy'").Scan(&mode).Error,
			)
			require.Equal(t, "user_oauth", mode)
			require.NoError(
				t,
				db.Raw("SELECT logical_knowledge_id FROM feishu_personal_resources WHERE "+
					"import_id='legacy'").
					Scan(&kid).
					Error,
			)
			require.Equal(t, "K1", kid)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(up).Error }))
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(down).Error }))
			require.NoError(t, db.Exec(up).Error)
			insert := "INSERT INTO feishu_personal_imports(id,user_id,app_id,open_id,kb_id," +
				"document_token,resource_type,url,auth_mode,application_id) VALUES(? ," +
				"?,'cli_test',?,?,'resource','docx','https://test.invalid',?,?)"
			require.NoError(t, db.Exec(insert, "app", "A", "", "kb", "application_grant", "cli_test").Error)
			require.Error(
				t,
				db.Exec(insert, "fake-open", "A", "fake", "kb2", "application_grant", "cli_test").Error,
			)
			require.Error(
				t,
				db.Exec(insert, "duplicate", "B", "", "kb", "application_grant", "cli_test").Error,
			)
			require.NoError(
				t,
				db.Exec(insert, "other-kb", "B", "", "kb2", "application_grant", "cli_test").Error,
			)
			require.NoError(
				t,
				db.Exec(
					"INSERT INTO feishu_personal_resources(import_id,logical_knowledge_id) "+
						"VALUES('app','K2')",
				).Error,
			)
			attempt := "INSERT INTO feishu_personal_attempts(id,import_id," +
				"logical_knowledge_id,candidate_version_id,revision,caller_tenant_id," +
				"app_config_version,grant_epoch,auth_mode,application_id) VALUES(?," +
				"'app','K2',?,'1',1,'cfg',?,?,?)"
			require.Error(t, db.Exec(attempt, "wrong-mode", "c1", "user-grant", "user_oauth", "").Error)
			require.Error(
				t,
				db.Exec(
					attempt,
					"wrong-grant",
					"c2",
					"fake-user-grant",
					"application_grant",
					"cli_test",
				).Error,
			)
			require.Error(
				t,
				db.Exec(attempt, "wrong-app", "c3", "", "application_grant", "cli_other").Error,
			)
			require.NoError(t, db.Exec(attempt, "good", "c4", "", "application_grant", "cli_test").Error)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(down).Error }))
			require.True(t, db.Migrator().HasColumn("feishu_personal_imports", "auth_mode"))
			var count int64
			require.NoError(t, db.Table("feishu_personal_attempts").Where("id='good'").Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}
