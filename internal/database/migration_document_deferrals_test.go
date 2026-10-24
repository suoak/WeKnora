package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentDeferralSQLiteMigrationRetainsExistingState(t *testing.T) {
	root := sqliteRepoRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations/sqlite/003038_feishu_document_deferrals.up.sql"))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(root, "migrations/sqlite/003038_feishu_document_deferrals.down.sql"))
	require.NoError(t, err)
	for _, state := range []string{"empty", "policy", "pending"} {
		t.Run(state, func(t *testing.T) {
			db := openSQLiteDB(t, filepath.Join(t.TempDir(), "deferral.db"))
			_, err := db.Exec(`CREATE TABLE data_sources(
			 id TEXT, status TEXT, config TEXT, last_sync_cursor TEXT)`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO data_sources
			 VALUES('source','paused','{"scope":"kept"}','{"accepted":"kept"}')`)
			require.NoError(t, err)
			_, err = db.Exec(string(up))
			require.NoError(t, err)
			var status, config, cursor, policy string
			require.NoError(t, db.QueryRow(`SELECT status,config,last_sync_cursor,document_deferrals
 FROM data_sources`).
				Scan(&status, &config, &cursor, &policy))
			require.Equal(t, "paused", status)
			require.Equal(t, `{"scope":"kept"}`, config)
			require.Equal(t, `{"accepted":"kept"}`, cursor)
			require.Equal(t, "[]", policy)
			if state == "policy" {
				_, err = db.Exec(`UPDATE data_sources SET document_deferrals='[{"external_id":"pending"}]'`)
				require.NoError(t, err)
			}
			if state == "pending" {
				_, err = db.Exec(`UPDATE data_sources SET last_sync_cursor=
				 '{"connector_cursor":{"feishu_pending_documents_v1":{"identity":{"external_id":"pending"}}}}'`)
				require.NoError(t, err)
			}
			tx, err := db.Begin()
			require.NoError(t, err)
			_, err = tx.Exec(string(down))
			if state != "empty" {
				require.Error(t, err, "downgrade cannot discard policy or pending retry")
				require.NoError(t, tx.Rollback())
				require.NoError(t, db.QueryRow(`SELECT document_deferrals FROM data_sources`).Scan(&policy))
			} else {
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
				_, err = db.Exec(string(up))
				require.NoError(t, err, "empty-policy down/up must work")
			}
		})
	}
}
