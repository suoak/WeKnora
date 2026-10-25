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

func TestFeishuV2PrivatePortalAndMCPDirectory(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var db *gorm.DB
			if backend == "postgres" {
				db = syncdb.Open(t, backend)
			} else {
				var err error
				db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "private-portal.db")), &gorm.Config{})
				require.NoError(t, err)
				pool, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { _ = pool.Close() })
			}
			for _, ddl := range []string{
				"CREATE TABLE tenants(id bigint PRIMARY KEY,name TEXT,status TEXT,deleted_at TIMESTAMP)",
				"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id bigint,name TEXT,type TEXT," +
					"embedding_model_id TEXT,is_temporary BOOLEAN,deleted_at TIMESTAMP)",
				"CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id bigint,knowledge_base_id TEXT," +
					"type TEXT,parse_status TEXT,deleted_at TIMESTAMP)",
				"CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)",
				"INSERT INTO tenants VALUES(1,'Controlled workspace','active',NULL)",
				"INSERT INTO knowledge_bases VALUES('private',1,'PRIVATE_NAME','document','model',false,NULL)," +
					"('ordinary',1,'Ordinary','document','model',false,NULL)",
				"INSERT INTO knowledges VALUES('private-doc',1,'private','file','completed',NULL)," +
					"('ordinary-doc',1,'ordinary','file','completed',NULL)",
				"INSERT INTO feishu_private_kbs VALUES('private','A')",
			} {
				require.NoError(t, db.Exec(ddl).Error)
			}
			db = types.EnablePrivateKBGuards(db)
			t.Setenv("FEISHU_ACCESS_V2_ENABLED", "")
			for _, user := range []string{"A", "B"} {
				ctx := context.WithValue(context.Background(), types.UserIDContextKey, user)
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleAdmin})
				ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user})
				// User-scoped MCP credentials preserve the same human principal;
				// a tenant-wide key must not acquire that private scope.
				for _, machine := range []bool{false, true} {
					call := ctx
					if machine {
						call = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: user})
					}
					repo := NewEnterpriseAccessRepository(db)
					rows, err := repo.ListPortalMCPCandidates(call)
					require.NoError(t, err)
					kbCounts, fileCounts, err := (&portalRepository{db: db}).resourceCountMaps(call, []uint64{1})
					require.NoError(t, err)
					if user == "A" && !machine {
						require.Len(t, rows, 2)
						require.EqualValues(t, 2, kbCounts[1])
						require.EqualValues(t, 2, fileCounts[1])
					} else {
						require.Len(t, rows, 1)
						require.Equal(t, "ordinary", rows[0].ID)
						require.EqualValues(t, 1, kbCounts[1])
						require.EqualValues(t, 1, fileCounts[1])
						_, err = repo.LoadDocumentReadSnapshot(call, user, 0, "private")
						require.ErrorIs(t, err, types.ErrPrivateKnowledgeBase)
					}
				}
			}
		})
	}
}
