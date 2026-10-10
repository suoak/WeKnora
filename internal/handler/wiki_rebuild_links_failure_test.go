package handler

import (
	"bytes"
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newRebuildLinksHandler wires the real wiki page service over a real SQLite
// repository into the HTTP handler, so the response contract is asserted
// end-to-end instead of through a stubbed service.
func newRebuildLinksHandler(t *testing.T) (*gin.Engine, *gorm.DB, interfaces.WikiPageRepository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.WikiFolder{}, &types.WikiPage{}, &types.WikiPageRevision{}))

	ctx := context.Background()
	repo := repository.NewWikiPageRepository(db)
	now := time.Now()
	for _, page := range []*types.WikiPage{
		{ID: "page-a", Slug: "concept/a", Title: "A", Content: "see [[concept/b]]"},
		{ID: "page-b", Slug: "concept/b", Title: "B", Content: "nothing to link"},
	} {
		page.TenantID = 1
		page.KnowledgeBaseID = "kb-rebuild-links"
		page.PageType = types.WikiPageTypeConcept
		page.Status = types.WikiPageStatusPublished
		page.Version = 1
		page.CreatedAt = now
		page.UpdatedAt = now
		require.NoError(t, repo.Create(ctx, page))
	}

	handler := &WikiPageHandler{
		wikiService: service.NewWikiPageService(repo, nil, nil, nil, nil, nil),
		kbService: &stubKBService{get: func(_ context.Context, id string) (*types.KnowledgeBase, error) {
			return &types.KnowledgeBase{
				ID:               id,
				TenantID:         1,
				IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
			}, nil
		}},
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/knowledgebase/:kb_id/wiki/rebuild-links", func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		handler.RebuildLinks(c)
	})
	return engine, db, repo
}

func postRebuildLinks(engine *gin.Engine, contexts ...context.Context) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/knowledgebase/kb-rebuild-links/wiki/rebuild-links", nil)
	if len(contexts) > 0 {
		req = req.WithContext(contexts[0])
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestRebuildLinksHandlerReportsPartialWriteFailure(t *testing.T) {
	engine, db, _ := newRebuildLinksHandler(t)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_wiki_meta_update
		BEFORE UPDATE ON wiki_pages
		WHEN OLD.slug = 'concept/b'
		BEGIN
			SELECT RAISE(ABORT, 'WIKI_SQL_SECRET_CANARY');
		END`).Error)

	var captured bytes.Buffer
	sink := logrus.New()
	sink.SetOutput(&captured)
	ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(sink))
	recorder := postRebuildLinks(engine, ctx)
	require.NotContains(t, captured.String(), "WIKI_SQL_SECRET_CANARY")
	require.Contains(t, captured.String(), "failed_count=1 total_count=2")
	t.Logf("rebuild-links response: %d %s", recorder.Code, recorder.Body.String())

	require.Equal(t, http.StatusInternalServerError, recorder.Code,
		"a half-rebuilt link graph must not answer 200")
	require.NotContains(t, recorder.Body.String(), "Links rebuilt successfully")
	require.NotContains(t, recorder.Body.String(), "concept/b")
	require.NotContains(t, recorder.Body.String(), "WIKI_SQL_SECRET_CANARY")
	require.Contains(t, recorder.Body.String(), "WIKI_LINK_REBUILD_FAILED")
	require.Contains(t, recorder.Body.String(), `"failed_count":1`)
	require.Contains(t, recorder.Body.String(), `"total_count":2`)
}

func TestRebuildLinksHandlerReportsSuccessWhenEveryPageIsWritten(t *testing.T) {
	engine, _, _ := newRebuildLinksHandler(t)

	recorder := postRebuildLinks(engine)
	t.Logf("rebuild-links response: %d %s", recorder.Code, recorder.Body.String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Links rebuilt successfully")
}

func TestRebuildLinksHandlerSQLFailureRetainsWritesAndRetry(t *testing.T) {
	engine, db, repo := newRebuildLinksHandler(t)
	before, err := repo.ListAll(context.Background(), "kb-rebuild-links")
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_wiki_meta_update BEFORE UPDATE ON wiki_pages
  WHEN OLD.slug = 'concept/b' BEGIN SELECT RAISE(ABORT, 'WIKI_SQL_SECRET_CANARY'); END`).Error)
	failed := postRebuildLinks(engine)
	require.Equal(t, 500, failed.Code)
	require.NotContains(t, failed.Body.String(), "WIKI_SQL_SECRET_CANARY")
	a, err := repo.GetBySlug(context.Background(), "kb-rebuild-links", "concept/a")
	require.NoError(t, err)
	require.Equal(t, []string{"concept/b"}, []string(a.OutLinks), "successful metadata write was retained")
	b, err := repo.GetBySlug(context.Background(), "kb-rebuild-links", "concept/b")
	require.NoError(t, err)
	require.Empty(t, b.InLinks)
	require.NoError(t, db.Exec("DROP TRIGGER fail_wiki_meta_update").Error)
	require.Equal(t, 200, postRebuildLinks(engine).Code)
	require.Equal(t, 200, postRebuildLinks(engine).Code)
	after, err := repo.ListAll(context.Background(), "kb-rebuild-links")
	require.NoError(t, err)
	for i := range before {
		require.Equal(t, before[i].Content, after[i].Content)
		require.Equal(t, before[i].Version, after[i].Version)
		require.Equal(t, before[i].SourceRefs, after[i].SourceRefs)
		require.Equal(t, before[i].TenantID, after[i].TenantID)
	}
	b, err = repo.GetBySlug(context.Background(), "kb-rebuild-links", "concept/b")
	require.NoError(t, err)
	require.Equal(t, []string{"concept/a"}, []string(b.InLinks))
	t.Log("SQL ABORT -> HTTP500; successful writes retained; explicit retry -> HTTP200; metadata-only and idempotent")
}

func TestRebuildLinksHandlerTotalFailureAndLookupFailureAreSafe(t *testing.T) {
	engine, db, _ := newRebuildLinksHandler(t)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_all_wiki_meta BEFORE UPDATE ON wiki_pages
 BEGIN SELECT RAISE(ABORT, 'WIKI_SQL_SECRET_CANARY'); END`).Error)
	failed := postRebuildLinks(engine)
	require.Equal(t, 500, failed.Code)
	require.Contains(t, failed.Body.String(), `"failed_count":2`)
	require.NotContains(t, failed.Body.String(), "WIKI_SQL_SECRET_CANARY")
	require.NoError(t, db.Exec("DROP TRIGGER fail_all_wiki_meta").Error)
	require.NoError(t, db.Exec("ALTER TABLE wiki_pages RENAME TO unavailable_wiki_pages").Error)
	failed = postRebuildLinks(engine)
	require.Equal(t, 500, failed.Code)
	require.NotContains(t, failed.Body.String(), "no such table")
	require.NotContains(t, failed.Body.String(), "unavailable_wiki_pages")
	require.NoError(t, db.Exec("ALTER TABLE unavailable_wiki_pages RENAME TO wiki_pages").Error)
	require.Equal(t, 200, postRebuildLinks(engine).Code)
}

// Data scoping regression only: full user/KB authorization remains covered by
// the existing real access tests; this fixture's KB service is a stub.
func TestRebuildLinksDoesNotWriteAnotherTenantKnowledgeBase(t *testing.T) {
	engine, _, repo := newRebuildLinksHandler(t)
	other := &types.WikiPage{ID: "tenant-b-page", TenantID: 2, KnowledgeBaseID: "tenant-b-kb", Slug: "concept/tenant-b",
		Content: "other tenant", Version: 9, PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished,
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, repo.Create(context.Background(), other))
	storedBefore, err := repo.GetBySlug(context.Background(), "tenant-b-kb", "concept/tenant-b")
	require.NoError(t, err)
	require.Equal(t, 200, postRebuildLinks(engine).Code)
	got, err := repo.GetBySlug(context.Background(), "tenant-b-kb", "concept/tenant-b")
	require.NoError(t, err)
	require.Equal(t, other.Content, got.Content)
	require.Equal(t, other.Version, got.Version)
	require.Equal(t, storedBefore.UpdatedAt, got.UpdatedAt)
	require.Empty(t, got.InLinks)
}

func TestRebuildLinksHandlerCancelledContextIsNotSuccess(t *testing.T) {
	engine, _, _ := newRebuildLinksHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := postRebuildLinks(engine, ctx)
	require.Equal(t, 500, response.Code)
	require.Contains(t, response.Body.String(), "WIKI_LINK_REBUILD_FAILED")
	require.NotContains(t, response.Body.String(), "SELECT")
	require.NotContains(t, response.Body.String(), "Links rebuilt successfully")
}
