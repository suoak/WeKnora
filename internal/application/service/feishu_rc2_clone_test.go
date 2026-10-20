package service

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This characterizes preservation on the approved, network-isolated production
// snapshot. It is deliberately NOT a successful incremental-update contract.
// No Feishu HTTP request, parser, embedder, or physical file service is used.
func TestRC2LegacyCloneChangedRevisionPreservesPublishedKnowledge(t *testing.T) {
	dsn := os.Getenv("RC2_APPROVED_CLONE_DSN")
	if dsn == "" {
		t.Skip("approved isolated production clone not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	defer conn.Close()
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.Equal(t, "rc2baseline", name, "only the dedicated clone is permitted")
	var sources []types.DataSource
	require.NoError(t, db.Where("type IN ? AND status = ? AND deleted_at IS NULL", []string{"feishu", "feishu_drive"}, "active").Order("id").Find(&sources).Error)
	require.NotEmpty(t, sources)
	for _, source := range sources {
		t.Run(source.Type+"/"+source.ID, func(t *testing.T) {
			var old types.Knowledge
			err := db.Where("tenant_id = ? AND knowledge_base_id = ? AND metadata->>'datasource_id' = ? AND COALESCE(metadata->>'external_id','') <> '' AND deleted_at IS NULL", source.TenantID, source.KnowledgeBaseID, source.ID).Order("id").First(&old).Error
			if err == gorm.ErrRecordNotFound {
				t.Skip("source has no retained external knowledge in this snapshot")
			}
			require.NoError(t, err)
			var external string
			require.NoError(t, db.Raw("SELECT metadata->>'external_id' FROM knowledges WHERE id = ?", old.ID).Scan(&external).Error)
			fingerprint := func() string {
				var value string
				require.NoError(t, db.Raw(`SELECT jsonb_build_object(
					'knowledge',(SELECT md5(to_jsonb(k)::text)FROM knowledges k WHERE id=?),
					'chunks',(SELECT md5(string_agg(id::text,',' ORDER BY id))FROM chunks WHERE knowledge_id=?),
					'embeddings',(SELECT count(*)FROM embeddings WHERE knowledge_id=?))::text`, old.ID, old.ID, old.ID).Scan(&value).Error)
				return value
			}
			before := fingerprint()
			ks := &sweepFakeKS{repo: repository.NewKnowledgeRepository(db)}
			svc := &DataSourceService{knowledgeService: ks}
			for i := 0; i < 2; i++ {
				item := types.FetchedItem{ExternalID: external, FileName: old.FileName, Content: []byte("RC2 synthetic changed revision; never published")}
				updated, err := svc.ingestItem(context.Background(), &source, &item, nil)
				require.ErrorIs(t, err, ErrFeishuStagingRequired)
				require.False(t, updated)
			}
			require.Empty(t, ks.events, "no replacement, reparse, or deletion permitted")
			require.Empty(t, ks.deleted)
			require.Equal(t, before, fingerprint(), "published identity, ACL scope and index rows must survive")
			result := &types.SyncResult{}
			originalCursor := string(source.LastSyncCursor)
			h := newStreamHandler(svc, &source, result, &types.SyncLog{})
			require.ErrorIs(t, h.Emit(context.Background(), types.FetchedItem{ExternalID: external, FileName: old.FileName, Content: []byte("RC2 synthetic changed revision; never published")}), ErrFeishuStagingRequired)
			require.Error(t, h.Checkpoint(context.Background(), &types.SyncCursor{}))
			require.Equal(t, originalCursor, string(source.LastSyncCursor))
			require.Equal(t, 1, result.Failed)
			require.Zero(t, result.Updated)
			require.Equal(t, before, fingerprint())
		})
	}
}
