package types

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWikiDeletionScopeDoesNotGrantSourceBodyRead(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cleanup.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	for _, sql := range []string{
		`CREATE TABLE tenants(id integer,status text,deleted_at datetime)`,
		`INSERT INTO tenants VALUES(7,'active',NULL)`,
		`CREATE TABLE knowledge_bases(id text,tenant_id integer,deleted_at datetime)`,
		`INSERT INTO knowledge_bases VALUES('kb',7,NULL),('other',7,NULL)`,
		`CREATE TABLE data_sources(id text,knowledge_base_id text,type text,legacy_access_state text)`,
		`INSERT INTO data_sources VALUES('held','kb','feishu','hold')`,
		`CREATE TABLE feishu_legacy_resources(knowledge_base_id text,knowledge_id text,state text)`,
		`CREATE TABLE knowledges(id text,knowledge_base_id text,channel text,metadata JSONB,deleted_at datetime)`,
		`INSERT INTO knowledges VALUES('doc','kb','feishu','{}',NULL)`,
		`CREATE TABLE task_pending_ops(scope text,scope_id text)`,
		`CREATE TABLE wiki_pages(id text,knowledge_base_id text)`,
		`INSERT INTO wiki_pages VALUES('page','kb'),('other-page','other')`,
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	db = EnableLegacyReadGuards(db)
	ctx := WithExecutionTenant(context.Background(), 7)
	ctx = WithLegacyWikiCleanup(ctx, 7, "kb")
	var rows []struct{ ID string }
	require.NoError(t, db.WithContext(ctx).Table("wiki_pages").Scan(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "page", rows[0].ID)
	require.ErrorIs(t, CheckLegacySourceRead(db.WithContext(ctx), "doc", "kb"), ErrLegacySourceRestricted)
	require.Empty(t, LegacyWikiCleanupKB(WithExecutionTenant(ctx, 99)))
}
