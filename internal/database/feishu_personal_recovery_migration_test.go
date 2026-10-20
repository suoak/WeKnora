package database

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The two DSNs must name new isolated DBs. The clone is restored from an
// existing OFFLINE 3030 dump; no test connects to a running company database.
func TestFeishuPersonalRecoveryMigrationsPostgres(t *testing.T) {
	root := sqliteRepoRoot(t)
	chdirAndRestore(t, root)
	for _, kind := range []string{"FRESH", "CLONE"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("FEISHU_PERSONAL_MIGRATION_" + kind + "_DSN")
			if dsn == "" {
				if os.Getenv("REQUIRE_PERSONAL_MIGRATION") == "1" {
					t.Fatal("isolated migration DSNs required")
				}
				t.Skip("isolated migration DB not configured")
			}
			require.Contains(t, dsn, "knowhub-p1-dispatch-pg-20261018")
			require.Contains(t, dsn, "/personal_")
			db, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			defer db.Close()
			if kind == "CLONE" {
				var version int
				var dirty bool
				require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
				require.Equal(t, 3030, version)
				require.False(t, dirty)
			}
			require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))
			var version int
			var dirty bool
			require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 3033, version)
			require.False(t, dirty)
			require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))
			var missing int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM feishu_personal_imports m LEFT JOIN feishu_personal_resources r ON r.import_id=m.id WHERE r.import_id IS NULL OR (m.knowledge_id<>'' AND (r.logical_knowledge_id<>m.knowledge_id OR r.active_version_id<>m.knowledge_id))").Scan(&missing))
			require.Zero(t, missing)
			var attempts int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM feishu_personal_attempts").Scan(&attempts))
			require.Zero(t, attempts)
			m, err := migrate.New("file://migrations/versioned", dsn)
			require.NoError(t, err)
			defer m.Close()
			require.NoError(t, m.Steps(-1))
			require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 3031, version)
			require.False(t, dirty)
			require.NoError(t, m.Steps(-1))
			require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 3030, version)
			require.False(t, dirty)
			require.NoError(t, m.Steps(1))
			require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 3031, version)
			require.False(t, dirty)
			require.NoError(t, m.Steps(1))
			require.NoError(t, db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 3033, version)
			require.False(t, dirty)
		})
	}
}

func TestFeishuPersonalRecoveryMigrationGuards(t *testing.T) {
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
			old, err := os.ReadFile(root + "/migrations/" + dir + "/003025_feishu_personal.up.sql")
			require.NoError(t, err)
			require.NoError(t, db.Exec(string(old)).Error)
			require.NoError(t, db.Exec("INSERT INTO users(id) VALUES('A')").Error)
			require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id) VALUES('kb')").Error)
			for _, id := range []string{"first", "second"} {
				require.NoError(t, db.Exec("INSERT INTO feishu_personal_imports(id,user_id,app_id,open_id,kb_id,document_token,resource_type,url,knowledge_id) VALUES(?,'A','app','open','kb',?,'file','https://test.invalid','legacy-K1')", id, id).Error)
			}
			up, err := os.ReadFile(root + "/migrations/" + dir + "/003031_feishu_personal_recovery.up.sql")
			require.NoError(t, err)
			// Conflicting legacy ownership fails atomically, never silently merges.
			err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(up)).Error })
			require.Error(t, err)
			require.False(t, db.Migrator().HasTable(&types.FeishuPersonalResource{}))
			require.NoError(t, db.Exec("DELETE FROM feishu_personal_imports WHERE id='second'").Error)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(up)).Error }))
			var resource types.FeishuPersonalResource
			require.NoError(t, db.First(&resource, "import_id = ?", "first").Error)
			require.Equal(t, "legacy-K1", resource.LogicalKnowledgeID)
			require.Equal(t, "legacy-K1", resource.ActiveVersionID)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(up)).Error }), "raw DDL repeat must fail rather than overwrite")
			a := types.FeishuPersonalAttempt{ID: uuid.NewString(), ImportID: "first", LogicalKnowledgeID: resource.LogicalKnowledgeID, CandidateVersionID: uuid.NewString(), Revision: "1", CallerTenantID: 1, AppConfigVersion: uuid.NewString(), GrantEpoch: uuid.NewString(), Status: "pending"}
			require.NoError(t, db.Omit("AuthMode", "ApplicationID").Create(&a).Error)
			down, err := os.ReadFile(root + "/migrations/" + dir + "/003031_feishu_personal_recovery.down.sql")
			require.NoError(t, err)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
			var count int64
			require.NoError(t, db.Model(&types.FeishuPersonalAttempt{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			// Test-only drain of synthetic state, then prove down/up compatibility.
			require.NoError(t, db.Delete(&a).Error)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(up)).Error }))
		})
	}
}
