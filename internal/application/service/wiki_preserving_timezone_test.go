package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func wikiPostgresWithScanTimezone(t *testing.T, db *gorm.DB, location *time.Location) *gorm.DB {
	t.Helper()
	dialector, ok := db.Dialector.(*postgres.Dialector)
	require.True(t, ok)
	config, err := pgx.ParseConfig(dialector.DSN)
	require.NoError(t, err)
	pool := stdlib.OpenDB(*config, stdlib.OptionAfterConnect(func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: "timestamptz", OID: pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: location},
		})
		return nil
	}))
	pool.SetMaxOpenConns(2)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	result, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	return result
}

func TestWikiPreservingFinishTimezonePostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires the real PostgreSQL recovery fixture")
	}
	for _, scenario := range []string{"unchanged", "changed_body", "changed_timestamp"} {
		t.Run(scenario, func(t *testing.T) {
			f := wikiPendingRegenerationFixture(t)
			f.PreparePublished(t)
			utc := wikiPostgresWithScanTimezone(t, f.DB, time.UTC)
			cst := wikiPostgresWithScanTimezone(t, f.DB, time.FixedZone("UTC8", 8*60*60))
			require.NoError(t, utc.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
			require.NoError(t, utc.Model(&types.KnowledgeBase{}).Where("id='public-two'").
				Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			stamp := time.Date(2026, 11, 4, 3, 14, 15, 123456000, time.UTC)
			old := types.WikiPage{
				ID: "timezone-historical", TenantID: 1, KnowledgeBaseID: "public-two",
				Slug: "concept/historical", Title: "Historical", Content: "HISTORICAL_BODY_REMAINS_BACKED_UP",
				PageType: "concept", Status: "published", Version: 2,
				SourceRefs: types.StringArray{"public-two-doc"}, CreatedAt: stamp, UpdatedAt: stamp,
			}
			require.NoError(t, utc.Create(&old).Error)
			require.NoError(t, utc.Create(&types.Chunk{
				ID: "timezone-deleted-source-chunk", TenantID: 1, KnowledgeBaseID: "public-two",
				KnowledgeID: "public-two-doc", Content: "DELETED_SOURCE_IS_STILL_A_PROOF_DEPENDENCY",
				ChunkType: "text", IsEnabled: true, DeletedAt: gorm.DeletedAt{Time: stamp, Valid: true},
			}).Error)
			ctx := context.Background()
			preview, err := repository.PrepareWikiPreservingRecovery(ctx, utc, 1, "public-two", "", false)
			require.NoError(t, err)
			_, err = repository.PrepareWikiPreservingRecovery(ctx, utc, 1, "public-two", preview.Snapshot, true)
			require.NoError(t, err)
			// Only this synthetic fixture models consumption of its own durable operation.
			require.NoError(t, utc.Where("scope='knowledge_base' AND scope_id='public-two'").
				Delete(&types.TaskPendingOp{}).Error)
			var scanned types.WikiPage
			require.NoError(t, cst.Session(&gorm.Session{SkipHooks: true}).First(&scanned, "id=?", old.ID).Error)
			require.True(t, scanned.CreatedAt.Equal(stamp))
			_, offset := scanned.CreatedAt.Zone()
			require.Equal(t, 8*60*60, offset, "exercise real pgx timestamp scanning, not a fake row")
			repo := repository.NewWikiPageRepository(cst)
			worker, err := repository.BeginWikiPreservingGeneration(ctx, repo, "public-two")
			require.NoError(t, err)
			require.NoError(t, repo.Create(worker, &types.WikiPage{
				ID: "timezone-current-summary", TenantID: 1, KnowledgeBaseID: "public-two",
				Slug: "summary/current", Title: "Current", Content: "CURRENT_PROVEN_SOURCE_SUMMARY",
				PageType: "summary", Status: "published", Version: 1,
				SourceRefs: types.StringArray{"public-two-doc"},
			}))
			switch scenario {
			case "changed_body":
				require.NoError(t, utc.Exec(
					"UPDATE wiki_pages SET content='CONCURRENT_OWNER_EDIT' WHERE id=?", old.ID).Error)
			case "changed_timestamp":
				require.NoError(t, utc.Exec(
					"UPDATE wiki_pages SET updated_at=updated_at+interval '1 microsecond' WHERE id=?",
					old.ID).Error)
			}
			finish, err := repository.FinishWikiPreservingRecovery(ctx, cst, 1, "public-two", "", false)
			if scenario != "unchanged" {
				require.ErrorContains(t, err, "historical Wiki page changed")
				var retained types.WikiPage
				require.NoError(t, utc.Session(&gorm.Session{SkipHooks: true}).First(&retained, "id=?", old.ID).Error)
				require.False(t, retained.DeletedAt.Valid)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{old.ID}, finish.RetiredPages)
			require.Equal(t, 1, finish.RetainedPages)
			applied, err := repository.FinishWikiPreservingRecovery(ctx, cst, 1, "public-two", finish.Snapshot, true)
			require.NoError(t, err)
			require.True(t, applied.Applied)
			var retired types.WikiPage
			require.NoError(t, utc.Unscoped().Session(&gorm.Session{SkipHooks: true}).
				First(&retired, "id=?", old.ID).Error)
			require.True(t, retired.DeletedAt.Valid)
			require.Equal(t, old.Content, retired.Content)
		})
	}
}
