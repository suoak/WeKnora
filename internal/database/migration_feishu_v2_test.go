package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeishuV2SQLiteInlinePublicationPreservesOldBatchesAndAudits(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, state := range []string{"old_unconfirmed", "consent", "binding"} {
		t.Run(state, func(t *testing.T) {
			db := openSQLiteDB(t, filepath.Join(t.TempDir(), "publication.db"))
			for _, sql := range []string{
				"CREATE TABLE feishu_import_batches(id TEXT PRIMARY KEY)",
				"CREATE TABLE feishu_import_batch_items(id TEXT PRIMARY KEY)",
				"CREATE TABLE feishu_sharing_approvals(id TEXT PRIMARY KEY)",
				"INSERT INTO feishu_import_batches VALUES('old-batch')",
				"INSERT INTO feishu_import_batch_items VALUES('old-item')",
				"INSERT INTO feishu_sharing_approvals VALUES('retained-audit')",
			} {
				_, err := db.Exec(sql)
				require.NoError(t, err)
			}
			up, err := os.ReadFile(
				filepath.Join(root, "migrations/sqlite/003042_feishu_inline_publication.up.sql"),
			)
			require.NoError(t, err)
			_, err = db.Exec(string(up))
			require.NoError(t, err)
			var unconfirmed bool
			require.NoError(
				t,
				db.QueryRow("SELECT publication_consent_at IS NULL FROM feishu_import_batches").
					Scan(&unconfirmed),
			)
			require.True(t, unconfirmed, "migration cannot infer consent for old submissions")
			if state == "consent" {
				_, err = db.Exec("UPDATE feishu_import_batches SET publication_consent_at=CURRENT_TIMESTAMP")
				require.NoError(t, err)
			}
			if state == "binding" {
				_, err = db.Exec("UPDATE feishu_import_batch_items SET sharing_approval_id='retained-audit'")
				require.NoError(t, err)
			}
			down, err := os.ReadFile(
				filepath.Join(root, "migrations/sqlite/003042_feishu_inline_publication.down.sql"),
			)
			require.NoError(t, err)
			tx, err := db.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(string(down))
			if state == "old_unconfirmed" {
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			} else {
				require.Error(t, err)
				require.NoError(t, tx.Rollback())
			}
			var audits int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM feishu_sharing_approvals").Scan(&audits))
			require.Equal(t, 1, audits)
		})
	}
}

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
				require.NoError(
					t,
					db.QueryRow("SELECT result FROM feishu_permission_collaboration").Scan(&result),
				)
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
			require.NoError(
				t,
				db.QueryRow("SELECT status,last_sync_cursor FROM data_sources").Scan(&status, &cursor),
			)
			require.Equal(t, "paused", status)
			require.Equal(t, `{"checkpoint":"retained"}`, cursor)
		})
	}
}

func TestFeishuV2SQLiteCollectionRollbackRetainsDiscoveryAndOldSources(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, populated := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "discovery_state"}[populated], func(t *testing.T) {
			db := openSQLiteDB(t, filepath.Join(t.TempDir(), "collection.db"))
			for _, sql := range []string{
				"CREATE TABLE users(id TEXT PRIMARY KEY)",
				"CREATE TABLE tenants(id INTEGER PRIMARY KEY)",
				"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY)",
				"CREATE TABLE feishu_import_batches(id TEXT PRIMARY KEY)",
				"CREATE TABLE data_sources(id TEXT,status TEXT,last_sync_cursor TEXT)",
				"INSERT INTO users VALUES('A')", "INSERT INTO tenants VALUES(1)",
				"INSERT INTO knowledge_bases VALUES('kb')",
				`INSERT INTO data_sources VALUES('protected','paused','{"retained":true}')`,
			} {
				_, err := db.Exec(sql)
				require.NoError(t, err)
			}
			up, err := os.ReadFile(filepath.Join(root, "migrations/sqlite/003041_feishu_collections.up.sql"))
			require.NoError(t, err)
			_, err = db.Exec(string(up))
			require.NoError(t, err)
			if populated {
				_, err = db.Exec(`INSERT INTO feishu_collections(id,owner_id,tenant_id,kb_id,auth_mode,
				connection_id,config_version,kind,root_id,state,frontier)
				VALUES('collection','A',1,'kb','user_oauth','ref','v1','drive','root','hold','encrypted-state')`)
				require.NoError(t, err)
			}
			down, err := os.ReadFile(
				filepath.Join(root, "migrations/sqlite/003041_feishu_collections.down.sql"),
			)
			require.NoError(t, err)
			tx, err := db.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(string(down))
			if populated {
				require.Error(t, err)
				require.NoError(t, tx.Rollback())
				var state string
				require.NoError(t, db.QueryRow("SELECT state FROM feishu_collections").Scan(&state))
				require.Equal(t, "hold", state)
			} else {
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}
			var status, cursor string
			require.NoError(
				t,
				db.QueryRow("SELECT status,last_sync_cursor FROM data_sources").Scan(&status, &cursor),
			)
			require.Equal(t, "paused", status)
			require.Equal(t, `{"retained":true}`, cursor)
		})
	}
}
