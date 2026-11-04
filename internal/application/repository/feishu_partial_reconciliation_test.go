package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSourcePartialReconciliationPreservesFailuresAndPublicProofs(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, scenario := range []string{"unpublished_only", "unproved_public", "held_resource", "authority_changed"} {
			t.Run(dialect+"/"+scenario, func(t *testing.T) {
				db := syncdb.Open(t, dialect)
				type publicRow struct {
					ID              string `gorm:"primaryKey"`
					TenantID        uint64
					KnowledgeBaseID string
					Channel         string
					Metadata        types.JSON `gorm:"type:jsonb"`
					Content         string
					DeletedAt       gorm.DeletedAt
				}
				require.NoError(t, db.Table("knowledges").AutoMigrate(&publicRow{}))
				require.NoError(t, db.AutoMigrate(&types.FeishuLegacyResource{}, &types.FeishuLegacyAttempt{}))
				ds := &types.DataSource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: uuid.NewString(),
					Type: "feishu", Status: "active", LegacyAccessState: "hold",
				}
				require.NoError(t, db.Create(ds).Error)
				logs := &SyncLogRepository{db: db}
				log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
				created, err := logs.CreateIfNoRunning(context.Background(), log)
				require.NoError(t, err)
				require.True(t, created)
				execution, claimed, err := logs.ClaimExecution(context.Background(), ds.ID, log.ID, 1)
				require.NoError(t, err)
				require.True(t, claimed)
				published := publicRow{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: ds.KnowledgeBaseID,
					Channel: "feishu", Metadata: types.JSON(`{"datasource_id":"` + ds.ID + `"}`),
					Content: "verified public body canary",
				}
				require.NoError(t, db.Table("knowledges").Create(&published).Error)
				receipt := types.FeishuLegacyReceipt{
					ResourceID: uuid.NewString(), AttemptID: uuid.NewString(), KnowledgeID: published.ID,
					Generation: 1, Chunks: 1, ManifestSHA256: strings.Repeat("a", 64),
				}
				data, err := json.Marshal(receipt)
				require.NoError(t, err)
				resource := types.FeishuLegacyResource{
					ID: receipt.ResourceID, TenantID: 1, KnowledgeBaseID: ds.KnowledgeBaseID,
					DataSourceID: ds.ID, KnowledgeID: published.ID, State: "active", ActiveAttempt: receipt.AttemptID,
				}
				require.NoError(t, db.Create(&resource).Error)
				require.NoError(t, db.Create(&types.FeishuLegacyAttempt{
					ID: receipt.AttemptID, ResourceID: resource.ID, Generation: 1,
					Status: "published", Receipt: data, Manifest: json.RawMessage(`{}`),
				}).Error)
				pending := types.FeishuLegacyResource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: ds.KnowledgeBaseID,
					DataSourceID: ds.ID, KnowledgeID: uuid.NewString(), State: "active", Generation: 3,
					CurrentAttempt: uuid.NewString(), SourceRevision: "2026-10-01T00:00:00Z",
				}
				if scenario == "held_resource" {
					pending.State = "hold"
				}
				require.NoError(t, db.Create(&pending).Error)
				if scenario == "unproved_public" {
					unknown := published
					unknown.ID = uuid.NewString()
					require.NoError(t, db.Table("knowledges").Create(&unknown).Error)
				}
				ctx := types.WithLegacyReceiptSet(types.WithSyncExecution(context.Background(), execution))
				ctx = types.WithLegacyReconciliationRead(ctx)
				ctx = types.WithReconciliationCommitGuard(ctx, func(*gorm.DB) error { return nil })
				ctx = types.WithFeishuSourceCommitGuard(ctx, func(*gorm.DB) error {
					if scenario == "authority_changed" {
						return errors.New("current source authority denied")
					}
					return nil
				})
				types.AddLegacyReceipt(ctx, receipt)
				ctx = types.WithLegacyFullReconciliation(ctx)
				log.Status, log.ItemsSkipped, log.ItemsFailed = types.SyncLogStatusPartial, 1, 1
				finished := time.Now().UTC()
				log.FinishedAt = &finished
				written, err := logs.WriteExecution(ctx, execution, ds, log, true)
				var stored types.DataSource
				require.NoError(t, db.First(&stored, "id=?", ds.ID).Error)
				var storedRun types.SyncLog
				require.NoError(t, db.First(&storedRun, "id=?", log.ID).Error)
				if scenario == "unpublished_only" {
					require.NoError(t, err)
					require.True(t, written)
					require.Equal(t, "active", stored.LegacyAccessState)
					require.Equal(t, types.SyncLogStatusPartial, storedRun.Status)
					require.Equal(t, 1, storedRun.ItemsFailed)
					require.Nil(t, stored.LastSyncAt, "partial recovery never becomes a complete sync")
				} else {
					require.Error(t, err)
					require.False(t, written)
					require.Equal(t, "hold", stored.LegacyAccessState)
					require.Equal(t, types.SyncLogStatusRunning, storedRun.Status)
				}
				var unchanged types.FeishuLegacyResource
				require.NoError(t, db.First(&unchanged, "id=?", pending.ID).Error)
				require.Equal(t, pending.CurrentAttempt, unchanged.CurrentAttempt)
				require.Equal(t, pending.Generation, unchanged.Generation)
				require.Empty(t, unchanged.ActiveAttempt, "failed candidate must not become readable")
				var body publicRow
				require.NoError(t, db.Table("knowledges").Where("id=?", published.ID).Take(&body).Error)
				require.Equal(t, published.Content, body.Content)
			})
		}
	}
}
