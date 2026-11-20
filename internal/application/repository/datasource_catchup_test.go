package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestScheduledSourceCandidatesExcludeDeletedParentsAndNeverLoadCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "scheduled.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.KnowledgeBase{}, &types.DataSource{}))
	for _, id := range []uint64{1, 2, 3} {
		require.NoError(t, db.Create(&types.Tenant{ID: id, Name: "scheduled", Status: "active"}).Error)
	}
	for _, kb := range []*types.KnowledgeBase{
		{ID: "live", TenantID: 1},
		{ID: "deleted-kb", TenantID: 1},
		{ID: "deleted-tenant", TenantID: 2},
		{ID: "inactive-tenant", TenantID: 3},
	} {
		require.NoError(t, db.Create(kb).Error)
	}
	require.NoError(t, db.Exec("UPDATE tenants SET deleted_at=CURRENT_TIMESTAMP WHERE id=2").Error)
	require.NoError(t, db.Exec("UPDATE tenants SET status='inactive' WHERE id=3").Error)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='deleted-kb'").Error)
	for _, scenario := range []string{
		"00-active", "01-error", "paused", "manual", "hold", "revoked", "deleted-kb",
		"deleted-tenant", "inactive-tenant", "missing-kb", "foreign-parent", "deleted-source",
	} {
		row := &types.DataSource{
			ID: scenario, TenantID: 1, KnowledgeBaseID: "live", Type: "gitlab", Status: "active",
			SyncSchedule: "@every 6h", CreatedAt: time.Now().UTC().Add(-24 * time.Hour),
			Config: types.JSON(`{"credentials":{"test":"not-selected"}}`),
		}
		switch scenario {
		case "01-error":
			row.Status = "error"
		case "paused":
			row.Status = "paused"
		case "manual":
			row.SyncSchedule = ""
		case "hold", "revoked":
			row.LegacyAccessState = scenario
		case "deleted-kb", "missing-kb":
			row.KnowledgeBaseID = scenario
		case "deleted-tenant":
			row.TenantID, row.KnowledgeBaseID = 2, scenario
		case "inactive-tenant":
			row.TenantID, row.KnowledgeBaseID = 3, scenario
		case "foreign-parent":
			row.TenantID = 2
		}
		require.NoError(t, db.Create(row).Error)
		if scenario == "deleted-source" {
			require.NoError(t, db.Delete(row).Error)
		}
	}
	repo := NewDataSourceRepository(db).(*DataSourceRepository)
	active, err := repo.FindActive(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 2)
	page, err := repo.ScheduledSyncCandidates(context.Background(), "", 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "00-active", page[0].ID)
	require.Empty(t, page[0].Config)
	page, err = repo.ScheduledSyncCandidates(context.Background(), page[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "01-error", page[0].ID)
	require.Empty(t, page[0].Config)
	page, err = repo.ScheduledSyncCandidates(context.Background(), page[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, page)
}
