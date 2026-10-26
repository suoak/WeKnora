package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWikiHistoryRegenerationIsolatesWithoutCertifyingOldText(t *testing.T) {
	f := fixture.New(t)
	testWikiHistoryRegeneration(t, f)
}

func testWikiHistoryRegeneration(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	repo := repository.NewWikiPageRepository(db)
	ctx := context.Background()
	old := &types.WikiPage{
		ID: "old", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "index",
		PageType: "index", Status: "published", Content: "UNVERIFIED_HISTORY_CANARY", Version: 9,
	}
	require.NoError(t, db.Create(old).Error)
	_, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.ErrorIs(t, err, repository.ErrWikiMCPUnverified)
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.False(t, plan.Applied)
	require.Equal(t, 1, plan.Documents)
	require.Equal(t, 1, plan.HistoricalPages)
	var n int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&n).Error)
	require.Zero(t, n)
	applied, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	var preserved types.WikiPage
	require.NoError(t, db.Unscoped().First(&preserved, "id='old'").Error)
	require.True(t, preserved.DeletedAt.Valid)
	require.Equal(t, old.Content, preserved.Content)
	require.NotEqual(t, "index", preserved.Slug)
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id='old'").Count(&n).Error)
	require.Zero(t, n, "old text must never receive a retrospective proof")
	var op types.TaskPendingOp
	require.NoError(t, db.First(&op).Error)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(op.Payload, &payload))
	require.Equal(t, "public-two-doc", payload["knowledge_id"])
	require.NotContains(t, string(op.Payload), "UNVERIFIED_HISTORY_CANARY")
	proof, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err, "empty generation can now start from current documents")
	newPage := &types.WikiPage{
		ID: "new", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/new",
		PageType: "concept", Status: "published", Content: "newly generated", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, newPage))
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id='new'").Count(&n).Error)
	require.EqualValues(t, 1, n)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.Error(t, err, "apply must not duplicate operations")
}

func TestWikiHistoryRegenerationRejectsChangedAndDeniedSources(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	db := f.DB
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").
		Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	ctx := context.Background()
	plan, err := repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE chunks SET content='changed input' WHERE knowledge_id='public-two-doc'").Error)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", plan.Snapshot, true)
	require.ErrorContains(t, err, "snapshot changed")
	_, err = repository.PrepareWikiRegeneration(ctx, db, 99, "public-two", "", false)
	require.Error(t, err)
	require.NoError(t, db.Exec("UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'").Error)
	_, err = repository.PrepareWikiRegeneration(ctx, db, 1, "public-two", "", false)
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestWikiSnapshotGenerationMergesOnlyBacklinks(t *testing.T) {
	f := fixture.New(t)
	testWikiSnapshotGeneration(t, f)
}

func testWikiSnapshotGeneration(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db := f.DB
	repo := repository.NewWikiPageRepository(db)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{
		ID: "snapshot-target", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "concept/target", Title: "target", PageType: "concept", Status: "published", Content: "initial",
		SourceRefs: types.StringArray{"public-two-doc"}, Version: 1,
	}
	require.NoError(t, repo.Create(ctx, page))
	svc := &wikiPageService{repo: repo}
	stale, err := svc.GetPageBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	backlinks := repo.(interface {
		UpdateBacklink(context.Context, string, string, string, bool) error
	})
	require.NoError(t, backlinks.UpdateBacklink(ctx, "public-two", page.Slug, "concept/other", true))
	// Public callers still conflict when ANY persisted dependency changed.
	stale.Content = "new model body"
	_, err = svc.UpdatePage(context.Background(), stale)
	require.ErrorIs(t, err, repository.ErrWikiPageConflict)
	updated, err := svc.UpdatePage(ctx, stale)
	require.NoError(t, err)
	require.Equal(t, "new model body", updated.Content)
	require.Contains(t, updated.InLinks, "concept/other")
	// A real body change must never be merged, even with a generation proof.
	stale = updated
	fresh, err := svc.GetPageBySlug(ctx, "public-two", page.Slug)
	require.NoError(t, err)
	fresh.Content = "intervening writer"
	_, err = svc.UpdatePage(ctx, fresh)
	require.NoError(t, err)
	stale.Content = "stale overwrite"
	_, err = svc.UpdatePage(ctx, stale)
	require.ErrorIs(t, err, repository.ErrWikiPageConflict)
}

func TestWikiHistoryPostgres(t *testing.T) {
	dsn := os.Getenv("WIKI_HISTORY_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated Wiki PostgreSQL fixture not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := admin.DB()
	require.NoError(t, err)
	defer func() { require.NoError(t, pool.Close()) }()
	for _, tc := range []struct {
		name string
		run  func(*testing.T, *fixture.Fixture)
	}{{"regeneration", testWikiHistoryRegeneration}, {"snapshot", testWikiSnapshotGeneration}} {
		t.Run(tc.name, func(t *testing.T) {
			name := "wiki_history_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			require.NoError(t, admin.Exec("CREATE DATABASE "+name).Error)
			t.Cleanup(func() { require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error) })
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			parsed.Path = "/" + name
			cwd, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(filepath.Join(cwd, "../../..")))
			err = database.RunMigrations(parsed.String())
			require.NoError(t, os.Chdir(cwd))
			require.NoError(t, err)
			db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			tc.run(t, fixture.Seed(t, db))
		})
	}
}
