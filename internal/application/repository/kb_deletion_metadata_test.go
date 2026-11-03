package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKBDeletionMetadataScope(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var db *gorm.DB
			if backend == "postgres" {
				db = syncdb.Open(t, backend)
				// syncdb owns this disposable schema and precreates the full source table.
				require.NoError(t, db.Exec("DROP TABLE data_sources").Error)
			} else {
				var err error
				db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "delete.db")), &gorm.Config{})
				require.NoError(t, err)
			}
			for _, ddl := range []string{
				"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id BIGINT,creator_id TEXT,name TEXT," +
					"vector_store_id TEXT,description TEXT,deleted_at TIMESTAMP)",
				"CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)",
				"CREATE TABLE feishu_legacy_resources(knowledge_base_id TEXT,knowledge_id TEXT,state TEXT)",
				"CREATE TABLE IF NOT EXISTS data_sources(knowledge_base_id TEXT,type TEXT,legacy_access_state TEXT)",
				"CREATE TABLE tenants(id BIGINT,status TEXT,deleted_at TIMESTAMP)",
				"CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id BIGINT,knowledge_base_id TEXT," +
					"embedding_model_id TEXT,type TEXT,file_path TEXT,storage_size BIGINT," +
					"description TEXT,deleted_at TIMESTAMP)",
				"INSERT INTO tenants VALUES(1,'active',NULL)",
				"INSERT INTO knowledge_bases VALUES('held',1,'owner','Name','store','SECRET',NULL)," +
					"('other',1,'owner','Other','store','SECRET',NULL)",
				"INSERT INTO data_sources(knowledge_base_id,type,legacy_access_state) " +
					"VALUES('held','feishu','revoked'),('other','feishu','hold')",
				"INSERT INTO knowledges VALUES('doc',1,'held','model','file','/file',10,'SECRET',NULL)",
			} {
				require.NoError(t, db.Exec(ddl).Error)
			}
			db = types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
			repo := &knowledgeBaseRepository{db: db}
			ctx := types.WithKBDeletionMetadata(context.Background(), "held")
			kb, err := repo.GetKnowledgeBaseByID(ctx, "held")
			require.NoError(t, err)
			require.Equal(t, "owner", kb.CreatorID)
			require.Empty(t, kb.Description, "administrative read must not load generated content")
			_, err = repo.GetKnowledgeBaseByID(context.Background(), "held")
			require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
			_, err = repo.GetKnowledgeBaseByID(ctx, "other")
			require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "proof is exact KB only")
			require.ErrorIs(t, types.CheckLegacySourceRead(db.WithContext(ctx), "", "held"),
				types.ErrLegacySourceRestricted)
			kg := &knowledgeRepository{db: db}
			_, err = kg.ListKnowledgeForKBDeletion(ctx, 1, "held")
			require.Error(t, err, "active parent cannot be cleaned up")
			require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='held'").Error)
			_, err = kg.ListKnowledgeForKBDeletion(ctx, 2, "held")
			require.Error(t, err, "wrong tenant cannot be cleaned up")
			rows, err := kg.ListKnowledgeForKBDeletion(ctx, 1, "held")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, "doc", rows[0].ID)
			require.Equal(t, "/file", rows[0].FilePath)
			require.Empty(t, rows[0].Description)
			require.NoError(t, db.Exec("INSERT INTO feishu_private_kbs VALUES('other','private-owner')").Error)
			_, err = repo.GetKnowledgeBaseByID(types.WithKBDeletionMetadata(ctx, "other"), "other")
			require.Error(t, err, "administrative deletion must not grant another user's private KB")
		})
	}
}
