package types

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPrivateKBGuardsDenyOtherUsersKeysAndWikiProjections(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "private.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	for _, sql := range []string{
		"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id INTEGER,creator_id TEXT,deleted_at DATETIME)",
		"CREATE TABLE knowledges(id TEXT PRIMARY KEY,knowledge_base_id TEXT,deleted_at DATETIME)",
		"CREATE TABLE chunks(id TEXT PRIMARY KEY,knowledge_id TEXT,knowledge_base_id TEXT)",
		"CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)",
		"CREATE TABLE wiki_pages(id TEXT PRIMARY KEY,knowledge_base_id TEXT)",
		"INSERT INTO knowledge_bases(id,tenant_id,creator_id) VALUES('private',1,'A'),('public',1,'A')",
		"INSERT INTO knowledges(id,knowledge_base_id) VALUES('doc','private')",
		"INSERT INTO chunks VALUES('chunk','doc','private')",
		"INSERT INTO feishu_private_kbs VALUES('private','A')",
		"INSERT INTO wiki_pages VALUES('page','private')",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	db = EnablePrivateKBGuards(db)
	for _, user := range []string{"A", "B", "system-1", ""} {
		ctx := context.WithValue(context.Background(), UserIDContextKey, user)
		call := db.WithContext(ctx)
		var kb KnowledgeBase
		err = call.Where("id = ?", "private").First(&kb).Error
		if user == "A" {
			require.NoError(t, err)
			require.Equal(t, "A", kb.PrivateOwnerID)
		} else {
			require.Error(t, err)
		}
		var doc Knowledge
		err = call.Where("id = ?", "doc").First(&doc).Error
		if user == "A" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		var projected []struct{ ID string }
		require.NoError(t, call.Table("wiki_pages").Select("id").Find(&projected).Error)
		if user == "A" {
			require.Len(t, projected, 1)
		} else {
			require.Empty(t, projected)
		}
		var public KnowledgeBase
		require.NoError(t, call.Where("id = ?", "public").First(&public).Error)
	}
	// The source feature switch is unrelated to persisted read protection.
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "")
	require.ErrorIs(
		t,
		CheckPrivateKBRead(db.WithContext(context.Background()), "doc", ""),
		ErrPrivateKnowledgeBase,
	)
}
