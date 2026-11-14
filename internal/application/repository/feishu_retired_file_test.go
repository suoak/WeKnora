package repository

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRetiredFileProtectionDoesNotDenyUnrelatedImages(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, deleted := range []bool{false, true} {
			t.Run(dialect+"/"+map[bool]string{false: "retired", true: "soft_deleted"}[deleted], func(t *testing.T) {
				db := syncdb.Open(t, dialect)
				require.NoError(t, db.AutoMigrate(&types.StoredResource{}))
				jsonType := "JSON"
				if dialect == "postgres" {
					jsonType = "JSONB"
				}
				for _, ddl := range []string{
					"CREATE TABLE knowledges(id TEXT,tenant_id BIGINT,knowledge_base_id TEXT," +
						"file_path TEXT,deleted_at TIMESTAMP,metadata " + jsonType + ")",
					"CREATE TABLE feishu_private_kbs(kb_id TEXT,owner_id TEXT)",
					"CREATE TABLE feishu_legacy_resources(id TEXT,tenant_id BIGINT,state TEXT," +
						"active_attempt TEXT,original_file_path TEXT,knowledge_base_id TEXT)",
					"CREATE TABLE feishu_legacy_attempts(id TEXT,resource_id TEXT,manifest " + jsonType + ")",
					"INSERT INTO feishu_private_kbs VALUES('retired','A'),('live','A')",
				} {
					require.NoError(t, db.Exec(ddl).Error)
				}
				retired := types.StoredResource{
					ID: "old-resource", Handle: "retiredabcdefghijklmno", TenantID: 1, Provider: "local",
					PhysicalPath: "local://1/live/retired/source.docx", LocationHash: "old-location",
					State: types.ResourceStateDeleted,
				}
				if deleted {
					retired.State = types.ResourceStateActive
					retired.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
				}
				require.NoError(t, db.Create(&retired).Error)
				reference := types.BuildResourcePath(retired.Handle)
				require.NoError(t, db.Exec("INSERT INTO knowledges VALUES('live',1,'live',?,NULL,'{}')",
					"local://1/live/source.docx").Error)
				require.NoError(t, db.Exec("INSERT INTO knowledges VALUES('old',1,'retired',?,NULL,'{}')",
					reference).Error)
				if dialect == "postgres" {
					require.NoError(t, db.Exec("INSERT INTO feishu_legacy_resources VALUES"+
						"('legacy',1,'active','old-attempt',?,'retired')", reference).Error)
					manifest, err := json.Marshal(map[string]any{
						"knowledge": map[string]string{"file_path": reference},
					})
					require.NoError(t, err)
					require.NoError(t, db.Exec("INSERT INTO feishu_legacy_attempts VALUES('old-attempt','legacy',?)",
						string(manifest)).Error)
					db = types.EnableLegacyReadGuards(db)
				}
				r := &tenantRepository{db: types.EnablePrivateKBGuards(db)}
				owner := context.WithValue(context.Background(), types.UserIDContextKey, "A")
				managed, err := r.CheckLegacyFileRead(owner, 1, "local://1/exports/independent.png")
				require.NoError(t, err, "a retained locator must not poison the entire tenant")
				require.False(t, managed)
				for _, path := range []string{
					retired.PhysicalPath, "local://1/live/retired/image.png",
					"local://1/live/retired/%2e%2e/retired/image.png",
				} {
					managed, err = r.CheckLegacyFileRead(owner, 1, path)
					require.True(t, managed)
					require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "retired bytes remain closed")
				}
				_, err = r.CheckLegacyFileRead(owner, 1, reference)
				require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
				managed, err = r.CheckLegacyFileRead(owner, 1, "local://1/live/image.png")
				require.True(t, managed)
				require.NoError(t, err)
				other := context.WithValue(context.Background(), types.UserIDContextKey, "B")
				_, err = r.CheckLegacyFileRead(other, 1, "local://1/live/image.png")
				require.ErrorIs(t, err, types.ErrPrivateKnowledgeBase)
			})
		}
	}
}

func TestRetiredFileMissingBasenameKeepsSymlinkDirectoryProtection(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	require.NoError(t, os.Mkdir(actual, 0o700))
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(actual, alias))
	path, err := canonicalLegacyFilePath(filepath.Join(alias, "gone-source.docx"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(actual, "gone-source.docx"), path)
}
