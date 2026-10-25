package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeishuV2SQLiteMigrationsPreservePrivacyAndAuditOnRollback(t *testing.T) {
	root := sqliteRepoRoot(t)
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(root, "migrations/sqlite", name))
		require.NoError(t, err)
		return string(body)
	}
	upAccess := read("003039_feishu_access_v2.up.sql")
	downAccess := read("003039_feishu_access_v2.down.sql")
	upCollaboration := read("003040_feishu_permission_collaboration.up.sql")
	downCollaboration := read("003040_feishu_permission_collaboration.down.sql")
	for _, state := range []string{"empty", "private", "collaboration"} {
		t.Run(state, func(t *testing.T) {
			db := openSQLiteDB(t, filepath.Join(t.TempDir(), "v2.db"))
			for _, ddl := range []string{
				"CREATE TABLE users(id TEXT PRIMARY KEY)",
				"CREATE TABLE tenants(id INTEGER PRIMARY KEY)",
				"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY)",
				"CREATE TABLE data_sources(id TEXT, status TEXT, last_sync_cursor TEXT)",
				"INSERT INTO users VALUES('A')",
				"INSERT INTO tenants VALUES(1)",
				"INSERT INTO knowledge_bases VALUES('kb')",
				`INSERT INTO data_sources VALUES('protected','paused','{"checkpoint":"retained"}')`,
				upAccess, upCollaboration,
			} {
				_, err := db.Exec(ddl)
				require.NoError(t, err)
			}
			if state == "private" {
				_, err := db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')")
				require.NoError(t, err)
			}
			if state == "collaboration" {
				for _, ddl := range []string{
					`INSERT INTO feishu_import_batches(id,owner_id,tenant_id,kb_id,auth_mode,connection_id,` +
						`config_version,idempotency_key,payload_hash) ` +
						`VALUES('batch','A',1,'kb','user_oauth','ref','version','key','hash')`,
					`INSERT INTO feishu_import_batch_items(id,batch_id,ordinal,url,status) ` +
						`VALUES('item','batch',0,'https://test.feishu.cn/file/real','blocked')`,
					`INSERT INTO feishu_permission_collaboration(id,batch_id,item_id,actor_id,action,result) ` +
						`VALUES('event','batch','item','A','reported','reported_unverified')`,
				} {
					_, err := db.Exec(ddl)
					require.NoError(t, err)
				}
			}
			tx, err := db.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(downCollaboration)
			if state == "collaboration" {
				require.Error(t, err)
				require.NoError(t, tx.Rollback())
				var result string
				require.NoError(t, db.QueryRow("SELECT result FROM feishu_permission_collaboration").Scan(&result))
				require.Equal(t, "reported_unverified", result)
			} else {
				require.NoError(t, err)
				_, err = tx.Exec(downAccess)
				if state == "private" {
					require.Error(t, err)
					require.NoError(t, tx.Rollback())
					var owner string
					require.NoError(t, db.QueryRow("SELECT owner_id FROM feishu_private_kbs").Scan(&owner))
					require.Equal(t, "A", owner)
				} else {
					require.NoError(t, err)
					require.NoError(t, tx.Commit())
				}
			}
			var status, cursor string
			require.NoError(t, db.QueryRow("SELECT status,last_sync_cursor FROM data_sources").Scan(&status, &cursor))
			require.Equal(t, "paused", status)
			require.Equal(t, `{"checkpoint":"retained"}`, cursor)
		})
	}
}
