package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSourceReconciliationInventoryCoverage(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			// Only metadata is needed; body/profile are intentionally absent.
			type inventoryRow struct {
				ID              string `gorm:"primaryKey"`
				TenantID        uint64
				KnowledgeBaseID string
				Channel         string
				Metadata        types.JSON
				DeletedAt       gorm.DeletedAt
			}
			require.NoError(t, db.Table("knowledges").AutoMigrate(&inventoryRow{}))
			require.NoError(t, db.AutoMigrate(&types.FeishuLegacyResource{}))
			ds := types.DataSource{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu"}
			require.NoError(t, db.Create(&ds).Error)
			ctx := types.WithLegacyReceiptSet(context.Background())
			e := types.SyncExecution{DataSourceID: ds.ID, TenantID: 1}
			check := func() error { return validateSourceReconciliationInventory(db, ctx, e) }
			require.NoError(t, check(), "a verified empty source has no historical public rows")
			row := inventoryRow{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Channel: "feishu",
				Metadata: types.JSON(`{"datasource_id":"` + ds.ID + `"}`),
			}
			require.NoError(t, db.Table("knowledges").Create(&row).Error)
			require.ErrorContains(t, check(), "source_reconciliation_inventory_incomplete")
			resource := types.FeishuLegacyResource{
				ID: uuid.NewString(), KnowledgeID: row.ID, TenantID: 1,
				KnowledgeBaseID: "kb", DataSourceID: ds.ID, ExternalID: "synthetic", State: "active",
			}
			require.NoError(t, db.Create(&resource).Error)
			require.Error(t, check(), "an old active resource is not proof of this full traversal")
			types.AddLegacyReceipt(ctx, types.FeishuLegacyReceipt{ResourceID: resource.ID, KnowledgeID: row.ID})
			require.NoError(t, check())
			unknown := inventoryRow{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb",
				Channel: "feishu", Metadata: types.JSON(`{}`),
			}
			require.NoError(t, db.Table("knowledges").Create(&unknown).Error)
			require.Error(t, check(), "unknown historical source ownership remains closed")
			require.NoError(t, db.Table("knowledges").Delete(&unknown).Error)
			require.NoError(t, check(), "soft-deleted history does not become visible by clearing source hold")
			manual := inventoryRow{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Channel: "web"}
			require.NoError(t, db.Table("knowledges").Create(&manual).Error)
			require.NoError(t, check(), "unrelated ordinary uploads are not attributed to this source")
		})
	}
}
