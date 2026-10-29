package repository

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeishuLegacyFormalMigrationChain(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	for _, dialect := range []string{"postgres", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			var dsn, source string
			var db *gorm.DB
			if dialect == "postgres" {
				dsn = os.Getenv("FEISHU_LEGACY_TEST_DSN")
				if dsn == "" {
					t.Skip("requires private feishu_atomic_test PostgreSQL")
				}
				require.Contains(t, dsn, "feishu_atomic_test")
				parsed, e := url.Parse(dsn)
				require.NoError(t, e)
				admin, e := sql.Open("pgx", dsn)
				require.NoError(t, e)
				defer admin.Close()
				name := "feishu_atomic_test_migration_" + uuid.NewString()[:8]
				_, e = admin.Exec("CREATE DATABASE " + name)
				require.NoError(t, e)
				parsed.Path = "/" + name
				dsn = parsed.String()
				source = "file://migrations/versioned"
				db, e = gorm.Open(postgres.Open(dsn), &gorm.Config{})
				require.NoError(t, e)
			} else {
				path := filepath.Join(t.TempDir(), "formal.db")
				dsn = "sqlite3://" + filepath.ToSlash(path)
				source = "file://migrations/sqlite"
				var e error
				db, e = gorm.Open(sqlite.Open(path), &gorm.Config{})
				require.NoError(t, e)
			}
			m, e := migrate.New(source, dsn)
			require.NoError(t, e)
			defer m.Close()
			// Official complete chain creates the genuine RC3 shape, including
			// production 3033 SQL. No Force, copied temporary chain or AutoMigrate.
			require.NoError(t, m.Migrate(3033))
			tenant := &types.Tenant{ID: 809903, Name: "migration-synthetic", Status: "active"}
			require.NoError(t, db.Create(tenant).Error)
			kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "historical", Type: "document"}
			require.NoError(t, db.Create(kb).Error)
			key := &types.TenantAPIKey{TenantID: &tenant.ID, Name: "old-key", KeyHash: uuid.NewString(), Capabilities: types.StringArray{"retrieve"}, KnowledgeBaseIDs: types.StringArray{kb.ID}}
			require.NoError(t, db.Omit("PersonalScopeMode").Create(key).Error)
			portal := &types.TenantPortalConfig{TenantID: tenant.ID, Status: types.PortalStatusPublished, DisplayName: "synthetic", CreatedBy: "fixture", UpdatedBy: "fixture"}
			require.NoError(t, db.Create(portal).Error)
			k := &types.Knowledge{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "historical", Type: "file", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
			require.NoError(t, db.Create(k).Error)
			// Real 3033 databases can have several Wiki pages per KB. Random
			// incarnation values must be generated after deduplicating KB IDs;
			// DISTINCT over (KB, random value) does not deduplicate anything.
			secondKB := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "second historical Wiki", Type: "document"}
			require.NoError(t, db.Create(secondKB).Error)
			for i, wikiKB := range []string{kb.ID, kb.ID, secondKB.ID} {
				page := &types.WikiPage{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: wikiKB, Slug: "historical/" + string(rune('a'+i)), Title: "historical synthetic", Content: "historical body remains unchanged", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{}, Version: 1}
				require.NoError(t, db.Create(page).Error)
			}
			// Compare persisted pre/post values (PostgreSQL stores microsecond
			// timestamps); comparing a newly allocated Go nanosecond value would
			// falsely attribute initial storage rounding to this migration.
			require.NoError(t, db.First(key, "id=?", key.ID).Error)
			key.PersonalScopeMode = "fixed" // new deny-default metadata; original authority is unchanged
			before, _ := json.Marshal(key)
			require.NoError(t, database.RunMigrations(dsn))
			// Deployment migrates before accepting traffic and restarts the APP.
			// Reopen the old application pool: SELECT * prepared at 3033 cannot
			// retain its old result shape after additive credential columns.
			oldPool, poolErr := db.DB()
			require.NoError(t, poolErr)
			require.NoError(t, oldPool.Close())
			if dialect == "postgres" {
				db, e = gorm.Open(postgres.Open(dsn), &gorm.Config{})
			} else {
				db, e = gorm.Open(sqlite.Open(strings.TrimPrefix(dsn, "sqlite3://")), &gorm.Config{})
			}
			require.NoError(t, e)
			version, dirty, e := m.Version()
			require.NoError(t, e)
			require.EqualValues(t, 3043, version)
			require.False(t, dirty)
			var saved types.TenantAPIKey
			var persistedMode string
			require.NoError(t, db.Raw("SELECT personal_scope_mode FROM tenant_api_keys WHERE id=?", key.ID).Scan(&persistedMode).Error)
			require.Equal(t, "fixed", persistedMode, "3037 must store the conservative historical default")
			require.NoError(t, db.First(&saved, "id=?", key.ID).Error)
			after, _ := json.Marshal(&saved)
			require.JSONEq(t, string(before), string(after), "existing credential values and authority remain unchanged")
			var public int64
			require.NoError(t, db.Table("feishu_legacy_resources").Count(&public).Error)
			require.Zero(t, public, "migration must not enroll or expose history")
			if dialect == "postgres" {
				var grants int64
				require.NoError(t, db.Table("feishu_derived_write_grants").Count(&grants).Error)
				require.Zero(t, grants, "migration must not manufacture derived write authority")
			} else {
				require.False(t, db.Migrator().HasTable("feishu_derived_write_grants"), "SQLite keeps its original enrolled-source denial")
			}
			var savedPortal types.TenantPortalConfig
			require.NoError(t, db.First(&savedPortal, "tenant_id=?", tenant.ID).Error)
			require.Equal(t, portal.Status, savedPortal.Status)
			var savedK types.Knowledge
			require.NoError(t, db.First(&savedK, "id=?", k.ID).Error)
			require.Equal(t, k.Title, savedK.Title)
			var proofs int64
			require.NoError(t, db.Table("wiki_mcp_provenance").Count(&proofs).Error)
			require.Zero(t, proofs, "historical pages must not acquire manufactured provenance")
			var epochCount, historicalPages, validEpochs int64
			require.NoError(t, db.Table("wiki_mcp_kb_epochs").Count(&epochCount).Error)
			require.EqualValues(t, 2, epochCount, "one epoch per distinct KB, not per Wiki page")
			require.NoError(t, db.Table("wiki_mcp_kb_epochs").Where("generation=1 AND length(incarnation)=32").Count(&validEpochs).Error)
			require.Equal(t, epochCount, validEpochs)
			require.NoError(t, db.Table("wiki_pages").Where("content=? AND status=?", "historical body remains unchanged", types.WikiPageStatusPublished).Count(&historicalPages).Error)
			require.EqualValues(t, 3, historicalPages, "migration must preserve every historical page without blessing it")
			owner := &types.User{ID: uuid.NewString(), Username: "migration-owner", Email: "migration@example.invalid", PasswordHash: "synthetic-only", TenantID: tenant.ID, IsActive: true}
			require.NoError(t, db.Create(owner).Error)
			dynamic := &types.TenantAPIKey{OwnerUserID: &owner.ID, Name: "dynamic-test", KeyHash: uuid.NewString(), ScopeType: "user_mcp", PersonalScopeMode: "dynamic_all", Capabilities: types.StringArray{"retrieve", "enterprise_retrieve"}}
			require.NoError(t, db.Create(dynamic).Error)
			// Exercise the real down script transactionally: refusal must precede
			// destructive DDL. This does not dirty migration metadata or use Force.
			downPath := "migrations/versioned/003037_personal_dynamic_wiki.down.sql"
			if dialect == "sqlite" {
				downPath = "migrations/sqlite/003037_personal_dynamic_wiki.down.sql"
			}
			down, readErr := os.ReadFile(downPath)
			require.NoError(t, readErr)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }), "dynamic keys block unsafe schema downgrade")
			require.NoError(t, db.First(&saved, "id=?", key.ID).Error)
			require.Equal(t, "fixed", saved.PersonalScopeMode)
			require.NoError(t, db.Where("id=?", dynamic.ID).Delete(&types.TenantAPIKey{}).Error)
			require.NoError(t, m.Migrate(3033), "empty policy/attempt state permits fixture rollback")
			require.NoError(t, database.RunMigrations(dsn))
			version, dirty, e = m.Version()
			require.NoError(t, e)
			require.EqualValues(t, 3043, version)
			require.False(t, dirty)
		})
	}
}
