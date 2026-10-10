package service

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const rebuildLinksTestKBID = "kb-rebuild-links"

// newRebuildLinksWiki seeds a three-page wiki in a real SQLite database:
// concept/a -> concept/b -> concept/c. No page has links stored yet, so every
// page must be written for the rebuild to be complete.
func newRebuildLinksWiki(
	t *testing.T,
) (context.Context, *gorm.DB, interfaces.WikiPageRepository, interfaces.WikiPageService) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.WikiFolder{}, &types.WikiPage{}, &types.WikiPageRevision{}))

	ctx := context.Background()
	repo := repository.NewWikiPageRepository(db)
	svc := NewWikiPageService(repo, nil, nil, nil, nil, nil)

	now := time.Now()
	for _, page := range []*types.WikiPage{
		{ID: "page-a", Slug: "concept/a", Title: "A", Content: "see [[concept/b]]"},
		{ID: "page-b", Slug: "concept/b", Title: "B", Content: "see [[concept/c]]"},
		{ID: "page-c", Slug: "concept/c", Title: "C", Content: "nothing to link"},
	} {
		page.TenantID = 1
		page.KnowledgeBaseID = rebuildLinksTestKBID
		page.PageType = types.WikiPageTypeConcept
		page.Status = types.WikiPageStatusPublished
		page.Version = 1
		page.CreatedAt = now
		page.UpdatedAt = now
		require.NoError(t, repo.Create(ctx, page))
	}

	return ctx, db, repo, svc
}

// failWikiMetaUpdatesWhen installs a SQLite trigger that makes every UPDATE on
// wiki_pages fail while `when` holds. The failure is raised by the database
// itself (RAISE(ABORT)), so it travels the real repository write path instead
// of being simulated by a mock.
func failWikiMetaUpdatesWhen(t *testing.T, db *gorm.DB, when string) {
	t.Helper()
	require.NoError(t, db.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_wiki_meta_update
		BEFORE UPDATE ON wiki_pages
		WHEN %s
		BEGIN
			SELECT RAISE(ABORT, 'WIKI_SQL_SECRET_CANARY');
		END`, when)).Error)
}

func TestRebuildLinksSucceedsWhenEveryPageIsWritten(t *testing.T) {
	ctx, _, repo, svc := newRebuildLinksWiki(t)

	require.NoError(t, svc.RebuildLinks(ctx, rebuildLinksTestKBID))

	b, err := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/b")
	require.NoError(t, err)
	require.Equal(t, []string{"concept/a"}, []string(b.InLinks))

	c, err := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/c")
	require.NoError(t, err)
	require.Equal(t, []string{"concept/b"}, []string(c.InLinks))
}

func TestRebuildLinksReportsPartialWriteFailure(t *testing.T) {
	ctx, db, repo, svc := newRebuildLinksWiki(t)
	// concept/b can no longer be written; the other two pages still can.
	failWikiMetaUpdatesWhen(t, db, "OLD.slug = 'concept/b'")

	err := svc.RebuildLinks(ctx, rebuildLinksTestKBID)

	require.Error(t, err, "one unwritable page must not surface as a successful rebuild")
	require.Contains(t, err.Error(), "1 of 3")
	require.NotContains(t, err.Error(), "concept/b")
	require.NotContains(t, err.Error(), "WIKI_SQL_SECRET_CANARY")

	// Rebuild stays best-effort: the writable pages were still refreshed.
	a, getErr := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/a")
	require.NoError(t, getErr)
	require.Equal(t, []string{"concept/b"}, []string(a.OutLinks))

	// concept/b keeps the stale links — the exact half-rebuilt state the
	// caller has to be told about.
	b, getErr := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/b")
	require.NoError(t, getErr)
	require.Empty(t, b.InLinks)
}

func TestRebuildLinksReportsTotalWriteFailure(t *testing.T) {
	ctx, db, repo, svc := newRebuildLinksWiki(t)
	failWikiMetaUpdatesWhen(t, db, "1 = 1")

	err := svc.RebuildLinks(ctx, rebuildLinksTestKBID)

	require.Error(t, err, "a rebuild that wrote nothing must not report success")
	require.Contains(t, err.Error(), "3 of 3")
	for _, slug := range []string{"concept/a", "concept/b", "concept/c"} {
		require.NotContains(t, err.Error(), slug)
	}
	require.NotContains(t, err.Error(), "WIKI_SQL_SECRET_CANARY")

	c, getErr := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/c")
	require.NoError(t, getErr)
	require.Empty(t, c.InLinks, "no page may be reported as rebuilt when the write failed")
}

// Real SQL trigger failures, captured application logs, metadata-only retry.
func TestRebuildLinksSafeLogsAndIdempotentRetry(t *testing.T) {
	ctx, db, repo, svc := newRebuildLinksWiki(t)
	var logs bytes.Buffer
	sink := logrus.New()
	sink.SetOutput(&logs)
	ctx = context.WithValue(ctx, types.LoggerContextKey, logrus.NewEntry(sink))
	before, err := repo.ListAll(ctx, rebuildLinksTestKBID)
	require.NoError(t, err)
	failWikiMetaUpdatesWhen(t, db, "OLD.slug = 'concept/b'")
	err = svc.RebuildLinks(ctx, rebuildLinksTestKBID)
	var partial *WikiRebuildLinksError
	require.ErrorAs(t, err, &partial)
	require.Equal(t, 1, partial.FailedCount)
	require.Equal(t, 3, partial.TotalCount)
	require.Len(t, partial.FailedPageRefs, 1)
	require.Regexp(t, `^[a-f0-9]{12}$`, partial.FailedPageRefs[0])
	require.Contains(t, logs.String(), "page_ref=")
	require.NotContains(t, logs.String(), "WIKI_SQL_SECRET_CANARY")
	require.NotContains(t, logs.String(), "concept/b")
	require.NoError(t, db.Exec("DROP TRIGGER fail_wiki_meta_update").Error)
	require.NoError(t, svc.RebuildLinks(ctx, rebuildLinksTestKBID))
	require.NoError(t, svc.RebuildLinks(ctx, rebuildLinksTestKBID))
	after, err := repo.ListAll(ctx, rebuildLinksTestKBID)
	require.NoError(t, err)
	require.Len(t, after, len(before))
	for i := range before {
		require.Equal(t, before[i].ID, after[i].ID)
		require.Equal(t, before[i].Content, after[i].Content)
		require.Equal(t, before[i].SourceRefs, after[i].SourceRefs)
		require.Equal(t, before[i].TenantID, after[i].TenantID)
		require.Equal(t, before[i].Version, after[i].Version)
	}
	b, err := repo.GetBySlug(ctx, rebuildLinksTestKBID, "concept/b")
	require.NoError(t, err)
	require.Equal(t, []string{"concept/a"}, []string(b.InLinks))
	t.Log("real SQLite ABORT: partial=1/3, successful writes retained; retry twice: no version/body/source_refs changes")
}

func TestRebuildLinksLookupFailureIsSafe(t *testing.T) {
	ctx, db, _, svc := newRebuildLinksWiki(t)
	require.NoError(t, db.Exec("ALTER TABLE wiki_pages RENAME TO unavailable_wiki_pages").Error)
	defer db.Exec("ALTER TABLE unavailable_wiki_pages RENAME TO wiki_pages")
	err := svc.RebuildLinks(ctx, rebuildLinksTestKBID)
	var failure *WikiRebuildLinksError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.LookupFailed)
	require.NotContains(t, err.Error(), "no such table")
}

func TestRebuildLinksCancellationPreservesClassification(t *testing.T) {
	ctx, _, _, svc := newRebuildLinksWiki(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	err := svc.RebuildLinks(ctx, rebuildLinksTestKBID)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "wiki link source lookup failed", err.Error())
}
