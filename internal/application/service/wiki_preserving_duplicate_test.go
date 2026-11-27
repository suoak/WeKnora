package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiPreservingIdenticalPendingDeliveries(t *testing.T) {
	testWikiPreservingIdenticalPendingDeliveries(t, fixture.New)
}

func TestWikiPreservingIdenticalPendingDeliveriesPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	testWikiPreservingIdenticalPendingDeliveries(t, wikiPendingRegenerationFixture)
}

func testWikiPreservingIdenticalPendingDeliveries(t *testing.T, newFixture func(*testing.T) *fixture.Fixture) {
	for _, scenario := range []string{"identical", "changed_counter", "claimed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.PreparePublished(t)
			db := f.DB
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
			require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
				Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			old := types.WikiPage{
				ID: "historical", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "old/concept",
				Title: "Legacy", Content: "retained browser body", PageType: "concept", Status: "published", Version: 2,
				SourceRefs: types.StringArray{"public-two-doc"},
			}
			require.NoError(t, db.Create(&old).Error)
			for i := 0; i < 3; i++ {
				op := types.TaskPendingOp{
					TenantID: 1, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: "public-two",
					Op: "ingest", DedupKey: "public-two-doc", FailCount: i,
					EnqueuedAt: time.Now().UTC().Add(time.Duration(i) * time.Second).Truncate(time.Second),
					Payload:    json.RawMessage(`{"op":"ingest","knowledge_id":"public-two-doc","attempt":1,"language":"zh-CN"}`),
				}
				require.NoError(t, db.Create(&op).Error)
			}
			ctx := context.Background()
			plan, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
			require.NoError(t, err)
			require.Equal(t, 3, plan.PendingOperations)
			require.Equal(t, []string{"public-two-doc"}, plan.Documents)
			var pending []types.TaskPendingOp
			require.NoError(t, db.Where("scope='knowledge_base'").Order("id").Find(&pending).Error)
			switch scenario {
			case "changed_counter":
				require.NoError(t, db.Model(&pending[2]).Update("fail_count", 3).Error)
			case "claimed":
				require.NoError(t, db.Model(&pending[2]).Update("claimed_at", time.Now().UTC()).Error)
			}
			var before []types.TaskPendingOp
			require.NoError(t, db.Where("scope='knowledge_base'").Order("id").Find(&before).Error)
			applied, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", plan.Snapshot, true)
			var markers int64
			require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("scope='wiki_preserving_recovery'").Count(&markers).Error)
			if scenario == "identical" {
				require.NoError(t, err)
				require.True(t, applied.Applied)
				require.EqualValues(t, 1, markers)
			} else {
				require.Error(t, err)
				require.Zero(t, markers)
				if scenario == "changed_counter" {
					require.ErrorContains(t, err, "snapshot changed")
				} else {
					require.ErrorContains(t, err, "not safely resumable")
				}
			}
			var after []types.TaskPendingOp
			require.NoError(t, db.Where("scope='knowledge_base'").Order("id").Find(&after).Error)
			require.Equal(t, before, after, "retain every delivery, original counter, payload, claim and timestamp")
			var page types.WikiPage
			require.NoError(t, db.First(&page, "id=?", old.ID).Error)
			require.Equal(t, old.Content, page.Content)
			require.False(t, page.DeletedAt.Valid)
			require.Equal(t, old.Status, page.Status)
		})
	}
}
