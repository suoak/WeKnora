package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func portalAssetFixture(t *testing.T) (*gorm.DB, *portalRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	for _, statement := range []string{
		`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME)`,
		`CREATE TABLE tenant_portal_configs(tenant_id INTEGER PRIMARY KEY,status TEXT,display_name TEXT,
			description TEXT,category TEXT,responsible_team TEXT,contact TEXT,featured BOOLEAN,display_order INTEGER,
			allow_access_request BOOLEAN,interaction_organization_id TEXT)`,
		`CREATE TABLE tenant_portal_stages(tenant_id INTEGER,stage_key TEXT,display_order INTEGER)`,
		`CREATE TABLE tenant_members(id TEXT,tenant_id INTEGER,user_id TEXT,role TEXT,status TEXT,deleted_at DATETIME)`,
		`CREATE TABLE tenant_access_requests(tenant_id INTEGER,applicant_user_id TEXT,status TEXT)`,
		`CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id INTEGER,type TEXT,is_temporary BOOLEAN,
			deleted_at DATETIME)`,
		`CREATE TABLE document_subject_grants(knowledge_id TEXT,user_id TEXT,operation TEXT,state TEXT)`,
		`INSERT INTO tenants VALUES(1,'active',NULL),(2,'active',NULL)`,
		`INSERT INTO tenant_portal_configs VALUES(1,'published','one','','','','',0,0,1,NULL),(2,'published','empty',
			'','','','',0,0,1,NULL)`,
		`INSERT INTO tenant_members VALUES('1',1,'member','viewer','active',NULL),('2',1,'owner','owner','active',
			NULL),('3',1,'restricted','viewer','active',NULL)`,
		`INSERT INTO document_subject_grants VALUES('private','restricted','read','revoked')`,
		`INSERT INTO knowledge_bases VALUES('doc',1,'document',0,NULL),('temp',1,'document',1,NULL),('deleted',1,
			'document',0,CURRENT_TIMESTAMP),('faq',1,'faq',0,NULL),('wiki',1,'wiki',0,NULL)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db, &portalRepository{db: db}
}

func TestPortalAssetCountsTypeStatusAndKnowledgeBaseMatrix(t *testing.T) {
	db, repo := portalAssetFixture(t)
	for _, kb := range []string{"doc", "temp", "deleted"} {
		for _, typ := range documentEntryTypes {
			for _, status := range []string{
				"pending", "processing", "finalizing", "completed", "failed", "cancelled", "deleting",
			} {
				k := &types.Knowledge{
					ID:              kb + typ + status,
					TenantID:        1,
					KnowledgeBaseID: kb,
					Type:            typ,
					ParseStatus:     status,
				}
				require.NoError(t, db.Create(k).Error)
			}
		}
	}
	require.NoError(t, db.Create(&types.Knowledge{
		ID:              "soft",
		TenantID:        1,
		KnowledgeBaseID: "doc",
		Type:            "file",
		ParseStatus:     "completed",
		DeletedAt:       gorm.DeletedAt{Time: time.Now(), Valid: true},
	}).Error)
	for _, kb := range []string{"faq", "wiki"} {
		require.NoError(t, db.Create(&types.Knowledge{
			ID:              kb,
			TenantID:        1,
			KnowledgeBaseID: kb,
			Type:            "file",
			ParseStatus:     "completed",
		}).Error)
	}
	ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
	rows, err := repo.ListPublished(ctx, "admin", 0, interfaces.PortalListQuery{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.EqualValues(t, 24, *rows[0].DocumentEntryCount)
	require.EqualValues(t, 12, *rows[0].UploadedFileCount)
	require.EqualValues(t, 10, rows[0].FileCount,
		"legacy includes file-backed rows in FAQ/Wiki and retains its status whitelist")
	require.EqualValues(t, 1, *rows[0].FAQKnowledgeBaseCount)
	require.EqualValues(t, 1, *rows[0].WikiKnowledgeBaseCount)
	require.EqualValues(t, 0, *rows[1].DocumentEntryCount)
	card, err := NewKnowledgeRepository(db).CountKnowledgeByKnowledgeBaseID(ctx, 1, "doc")
	require.NoError(t, err)
	require.EqualValues(t, *rows[0].DocumentEntryCount, card)
	_, total, err := NewKnowledgeRepository(db).ListPagedKnowledgeByKnowledgeBaseID(
		ctx, 1, "doc", &types.Pagination{Page: 1, PageSize: 100}, types.KnowledgeListFilter{},
	)
	require.NoError(t, err)
	require.EqualValues(t, card, total)
}

func TestPortalPublishesDocumentScaleWithoutContentOrDetailedPrivateAggregates(t *testing.T) {
	db, repo := portalAssetFixture(t)
	require.NoError(t, db.Create(&types.Knowledge{
		ID:              "private",
		TenantID:        1,
		KnowledgeBaseID: "doc",
		Type:            "manual",
		ParseStatus:     "failed",
	}).Error)
	for _, user := range []string{"member", "owner", "restricted", "discoverer"} {
		t.Run(user, func(t *testing.T) {
			rows, err := repo.ListPublished(context.Background(), user, 0, interfaces.PortalListQuery{})
			require.NoError(t, err)
			if user == "owner" {
				require.Equal(t, "space_total", rows[0].AssetCountScope)
				require.EqualValues(t, 1, *rows[0].DocumentEntryCount)
			} else {
				require.Equal(t, "published_summary", rows[0].AssetCountScope)
				require.EqualValues(t, 1, *rows[0].DocumentEntryCount)
				require.Nil(t, rows[0].UploadedFileCount)
				require.Nil(t, rows[0].FAQKnowledgeBaseCount)
				require.Nil(t, rows[0].WikiKnowledgeBaseCount)
			}
			if user == "discoverer" {
				require.Equal(t, types.PortalAccessDiscoverable, rows[0].AccessState)
				require.Empty(t, rows[0].CurrentRole, "published counts must not grant membership")
			}
			payload, err := json.Marshal(rows)
			require.NoError(t, err)
			require.NotContains(t, string(payload), `"private"`, "document IDs and content must not appear in discovery metadata")
			require.EqualValues(t, 0, rows[0].FileCount, "legacy field is not repurposed for manual entries")
		})
	}
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{KnowledgeBaseRestricted: true, KnowledgeBaseIDs: types.StringArray{"doc"}})
	rows, err := repo.ListPublished(ctx, "owner", 0, interfaces.PortalListQuery{})
	require.NoError(t, err)
	require.Equal(t, "hidden", rows[0].AssetCountScope)
	require.Nil(t, rows[0].DocumentEntryCount, "restricted API keys retain their existing aggregate boundary")
}

func TestPortalAssetCountsFollowCreateDeleteAndFolderScopes(t *testing.T) {
	db, repo := portalAssetFixture(t)
	ctx := context.Background()
	k := NewKnowledgeRepository(db)
	for i, typ := range []string{"file", "manual", "file_url", "url"} {
		require.NoError(t, k.CreateKnowledge(ctx, &types.Knowledge{
			ID:              fmt.Sprint(i),
			TenantID:        1,
			KnowledgeBaseID: "doc",
			Type:            typ,
			ParseStatus:     "completed",
			FolderPath:      []string{"", "", "sub", "sub/deep"}[i],
		}))
		counts, err := repo.documentAssetCountMap(ctx, []uint64{1})
		require.NoError(t, err)
		require.EqualValues(t, i+1, counts[1].Documents)
	}
	for _, tc := range []struct {
		scope  types.KnowledgeFolderScope
		path   string
		status string
		want   int64
	}{
		{types.FolderScopeExact, "", "", 2},
		{types.FolderScopeExact, "sub", "", 1},
		{types.FolderScopeSubtree, "", "", 4},
		{types.FolderScopeSubtree, "sub", "", 2},
		{types.FolderScopeSubtree, "", "failed", 0},
	} {
		_, total, err := k.ListPagedKnowledgeByKnowledgeBaseID(
			ctx, 1, "doc", &types.Pagination{Page: 1, PageSize: 10},
			types.KnowledgeListFilter{FolderScope: tc.scope, FolderPath: tc.path, ParseStatus: tc.status},
		)
		require.NoError(t, err)
		require.Equal(t, tc.want, total)
	}
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "0").Update("parse_status", "deleting").Error)
	require.NoError(t, db.Where("id = ?", "1").Delete(&types.Knowledge{}).Error)
	counts, err := repo.documentAssetCountMap(ctx, []uint64{1})
	require.NoError(t, err)
	require.EqualValues(t, 2, counts[1].Documents)
}
