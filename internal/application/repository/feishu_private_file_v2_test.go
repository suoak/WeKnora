package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeishuV2PrivateFilesRemainGuardedWithoutLegacyPlugin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "files.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	for _, ddl := range []string{
		"CREATE TABLE knowledges(id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, file_path TEXT, deleted_at DATETIME)",
		"CREATE TABLE feishu_private_kbs(kb_id TEXT, owner_id TEXT)",
		"INSERT INTO feishu_private_kbs VALUES('private','A')",
		"INSERT INTO knowledges VALUES('doc',1,'private','/isolated-test/private-doc/source.pdf',NULL)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	db = types.EnablePrivateKBGuards(db)
	require.False(t, types.LegacyReadGuardsEnabled(db))
	r := &tenantRepository{db: db}
	for _, user := range []string{"A", "B", "system-1", ""} {
		ctx := context.WithValue(context.Background(), types.UserIDContextKey, user)
		for _, path := range []string{"/isolated-test/private-doc/source.pdf", "/isolated-test/private-doc/image.png"} {
			bound, err := r.CheckLegacyFileRead(ctx, 1, path)
			require.True(t, bound)
			if user == "A" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, types.ErrPrivateKnowledgeBase)
			}
		}
	}
	bound, err := r.CheckLegacyFileRead(context.Background(), 1, "/isolated-test/ordinary/source.pdf")
	require.NoError(t, err)
	require.False(t, bound)
}
