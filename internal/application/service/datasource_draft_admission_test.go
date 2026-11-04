package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuUnusedDraftAdmission(t *testing.T) {
	testFeishuUnusedDraftAdmission(t, fixture.New(t))
}

func testFeishuUnusedDraftAdmission(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.SyncLog{}))
	for _, mode := range []string{"unused", "active", "held", "revoked", "failed_sync", "resource", "deleted_doc"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			source := &types.DataSource{
				ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "public-two", Name: "synthetic draft",
				Type: "feishu", Status: types.DataSourceStatusPaused, LegacyAccessState: "unmanaged",
				Config: types.JSON(`{"resource_ids":[]}`),
			}
			want := "hold"
			switch mode {
			case "unused":
				want = "unmanaged"
			case "active":
				source.Status = types.DataSourceStatusActive
			case "held":
				source.LegacyAccessState = "hold"
			case "revoked":
				source.LegacyAccessState, want = "revoked", "revoked"
			}
			require.NoError(t, repository.NewDataSourceRepository(db).Create(ctx, source))
			switch mode {
			case "failed_sync":
				require.NoError(t, db.Create(&types.SyncLog{
					ID: uuid.NewString(), TenantID: 1, DataSourceID: source.ID,
					Status: types.SyncLogStatusFailed, StartedAt: time.Now(),
				}).Error)
			case "resource":
				require.NoError(t, db.Create(&types.FeishuLegacyResource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: source.KnowledgeBaseID,
					DataSourceID: source.ID, ExternalID: "synthetic-old", KnowledgeID: uuid.NewString(),
					State: "hold", QuarantineSnapshot: json.RawMessage(`{}`),
				}).Error)
			case "deleted_doc":
				metadata, err := json.Marshal(map[string]string{"datasource_id": source.ID})
				require.NoError(t, err)
				require.NoError(t, db.Create(&types.Knowledge{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: source.KnowledgeBaseID,
					Type: "file", Metadata: types.JSON(metadata),
					DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
				}).Error)
			}
			source.Config = types.JSON(`{"resource_ids":["synthetic-selected-root"]}`)
			source.Status = types.DataSourceStatusActive
			guarded := types.EnableLegacyReadGuards(db)
			require.NoError(t, repository.NewDataSourceRepository(guarded).Update(ctx, source))
			stored, err := repository.NewDataSourceRepository(db).FindByID(ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, want, stored.LegacyAccessState)
			require.Equal(t, want, source.LegacyAccessState, "response must report the persisted protection state")
			require.JSONEq(t, string(source.Config), string(stored.Config))
		})
	}
}
