package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAuditFeishuOrphanFileLocatorBlocksUnrelatedTenantFiles(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.StoredResource{}))
	require.NoError(t, db.Exec(`CREATE TABLE feishu_legacy_resources(
id TEXT,tenant_id BIGINT,state TEXT,active_attempt TEXT,original_file_path TEXT,knowledge_base_id TEXT);
CREATE TABLE feishu_legacy_attempts(id TEXT,resource_id TEXT,manifest JSONB);
CREATE TABLE knowledges(id TEXT,tenant_id BIGINT,knowledge_base_id TEXT,file_path TEXT,metadata JSONB);
CREATE TABLE IF NOT EXISTS data_sources(id TEXT,type TEXT,legacy_access_state TEXT);`).Error)
	r := &tenantRepository{db: types.EnableLegacyReadGuards(db)}
	ctx := types.WithLocalSourceContentRead(context.Background())
	managed, err := r.CheckLegacyFileRead(ctx, 1, "local://1/independent/image.png")
	require.NoError(t, err)
	require.False(t, managed)
	missing := types.BuildResourcePath(strings.Repeat("a", types.ResourceHandleLength))
	require.NoError(t, db.Exec(`INSERT INTO feishu_legacy_resources VALUES
('orphan',1,'deleted','',?,'unrelated-kb')`, missing).Error)
	managed, err = r.CheckLegacyFileRead(ctx, 1, "local://1/independent/image.png")
	require.NoError(t, err)
	require.False(t, managed)
	require.NoError(t, db.Create(&types.DataSource{
		ID: "orphan-source", TenantID: 1, KnowledgeBaseID: "unrelated-kb",
		Name: "orphan", Type: "feishu", LegacyAccessState: "revoked",
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges VALUES
('orphan-document',1,'unrelated-kb',?,'{"datasource_id":"orphan-source"}')`, missing).Error)
	managed, err = r.CheckLegacyFileRead(ctx, 1, "local://1/independent/image.png")
	require.NoError(t, err)
	require.False(t, managed)
	for _, row := range []types.StoredResource{
		{ID: "remote",
			Handle:   strings.Repeat("b", types.ResourceHandleLength),
			TenantID: 1, Provider: "s3", PhysicalPath: "s3://bucket/key.docx",
			LocationHash: "remote", State: types.ResourceStateActive},
		{ID: "empty",
			Handle:   strings.Repeat("c", types.ResourceHandleLength),
			TenantID: 1, Provider: "local", LocationHash: "empty", State: types.ResourceStateDeleted},
		{ID: "foreign",
			Handle:   strings.Repeat("d", types.ResourceHandleLength),
			TenantID: 2, Provider: "local", PhysicalPath: "local://1/foreign/source.docx",
			LocationHash: "foreign", State: types.ResourceStateActive},
	} {
		require.NoError(t, db.Create(&row).Error)
		reference := types.BuildResourcePath(row.Handle)
		require.NoError(t, db.Exec(
			"INSERT INTO feishu_legacy_resources VALUES(?,1,'deleted','',?,'unrelated-kb')", row.ID, reference).Error)
		managed, err = r.CheckLegacyFileRead(ctx, 1, "local://1/independent/image.png")
		require.NoError(t, err)
		require.False(t, managed)
		_, err = r.CheckLegacyFileRead(ctx, 1, reference)
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	}
	managed, err = r.CheckLegacyFileRead(ctx, 1, "local://1/foreign/image.png")
	require.True(t, managed)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	_, err = r.CheckLegacyFileRead(ctx, 1, missing)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
}
