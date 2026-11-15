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

func TestSourceReparseMetadataUsesPersistedBindingsWithoutBodyReads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "protocol.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	for _, ddl := range []string{
		`CREATE TABLE knowledges(id text,tenant_id bigint,knowledge_base_id text,` +
			`channel text,metadata text,deleted_at text)`,
		`CREATE TABLE task_pending_ops(scope text,scope_id text,tenant_id bigint,payload text)`,
		`CREATE TABLE feishu_legacy_resources(knowledge_id text,tenant_id bigint,` +
			`knowledge_base_id text,data_source_id text)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledges VALUES
		('source',7,'kb','feishu','{"datasource_id":"source-id","external_id":"external"}',NULL),
		('ordinary',7,'kb','file','{}',NULL),
		('enrolled',7,'kb','file','{}',NULL),
		('deleted',7,'kb','feishu','{"datasource_id":"source-id","external_id":"external"}','deleted')`).Error)
	r := &knowledgeRepository{db: db}
	row, err := r.LoadPersonalReparseMetadata(context.Background(), "source")
	require.NoError(t, err)
	require.True(t, row.LegacySourceManaged)
	require.Equal(t, "source-id", row.DataSourceID)
	for _, id := range []string{"ordinary", "deleted", "missing", "enrolled"} {
		row, err := r.LoadPersonalReparseMetadata(context.Background(), id)
		require.NoError(t, err)
		require.Empty(t, row.ID)
	}
	require.NoError(t,
		db.Exec(`INSERT INTO feishu_legacy_resources VALUES ('enrolled',7,'kb','persisted-source')`).Error)
	types.EnableLegacyReadGuards(db)
	row, err = r.LoadPersonalReparseMetadata(context.Background(), "enrolled")
	require.NoError(t, err)
	require.True(t, row.LegacySourceManaged, "an enrolled resource cannot bypass admission by losing metadata")
	require.Equal(t, "persisted-source", row.DataSourceID)
}
