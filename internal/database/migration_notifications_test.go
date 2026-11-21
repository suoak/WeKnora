package database

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
)

func TestSQLiteNotificationMigrationPreservesOptOutsAndStages(t *testing.T) {
	chdirAndRestore(t, sqliteRepoRoot(t))
	db := openSQLiteDB(t, filepath.Join(t.TempDir(), "notifications.db"))
	driver, err := sqlite3migrate.WithInstance(db, &sqlite3migrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://migrations/sqlite", "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(3049))
	for _, query := range []string{
		`INSERT INTO tenants(id,name,business) VALUES(99,'portal-test','portal')`,
		`INSERT INTO users(id,username,email,password_hash,tenant_id) VALUES('portal-user','portal-user','portal@example.invalid','synthetic',99)`,
		`INSERT INTO tenant_portal_configs(tenant_id,status,display_name,allow_access_request,created_by,updated_by) VALUES(99,'published','portal-test',0,'portal-user','portal-user')`,
		`INSERT INTO tenant_portal_stages(tenant_id,stage_key,display_order) VALUES(99,'concept_product',10)`,
		`INSERT INTO tenant_access_requests(id,tenant_id,applicant_user_id,reason) VALUES('old-request',99,'portal-user','historical reason')`,
	} {
		_, err = db.Exec(query)
		require.NoError(t, err)
	}
	snapshots := map[string][32]byte{}
	for _, table := range []string{"tenant_portal_configs", "tenant_portal_stages", "tenant_access_requests"} {
		snapshots[table] = sqliteEnterpriseRowsDigest(t, db, table)
	}
	for _, version := range []uint{3050, 3049, 3050} {
		require.NoError(t, m.Migrate(version))
		for table, digest := range snapshots {
			require.Equal(t, digest, sqliteEnterpriseRowsDigest(t, db, table), table)
		}
		var defaultValue string
		rows, err := db.Query("SELECT dflt_value FROM pragma_table_info('tenant_portal_configs') WHERE name='allow_access_request'")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&defaultValue))
		require.NoError(t, rows.Close())
		if version == 3050 {
			require.Equal(t, "1", defaultValue)
			require.True(t, sqliteTableExists(t, db, "user_notifications"))
			_, err = db.Exec(`INSERT INTO user_notifications(id,user_id,notification_type,title,content,reference_type,reference_id,tenant_id) VALUES('n1','portal-user','submitted','title','body','tenant_access_request','old-request',99)`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO user_notifications(id,user_id,notification_type,title,content,reference_type,reference_id,tenant_id) VALUES('n2','portal-user','submitted','title','body','tenant_access_request','old-request',99)`)
			require.Error(t, err, "event uniqueness must be enforced by the real migration")
		} else {
			require.Equal(t, "0", defaultValue)
			require.False(t, sqliteTableExists(t, db, "user_notifications"))
		}
		var violations int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations))
		require.Zero(t, violations)
	}
}
