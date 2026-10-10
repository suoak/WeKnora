package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const lintKB = "synthetic-lint-kb"

// Only KB lookup is a synthetic adapter. All page reads/writes, lint,
// link rebuild and HTTP error mapping use real implementation paths.
// This fixture does not claim to validate the full authorization middleware.
type lintKBFixture struct {
	interfaces.KnowledgeBaseService
}

func (lintKBFixture) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: lintKB, TenantID: 7, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}}, nil
}

func (f lintKBFixture) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return f.GetKnowledgeBaseByIDOnly(ctx, id)
}

func newLintSQLFixture(t *testing.T) (*gorm.DB, interfaces.WikiPageRepository, *service.WikiLintService, *gin.Engine) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.WikiFolder{}, &types.WikiPage{}, &types.WikiPageRevision{}))
	repo := repository.NewWikiPageRepository(db)
	page := &types.WikiPage{ID: "lint-page", TenantID: 7, KnowledgeBaseID: lintKB, Slug: "concept/lint",
		Title: "Synthetic page", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished,
		Content: strings.Repeat("synthetic content ", 8) + "[[missing-target]]", OutLinks: types.StringArray{"missing-target"},
		Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, repo.Create(context.Background(), page))
	wiki := service.NewWikiPageService(repo, nil, nil, nil, nil, nil)
	kb := lintKBFixture{}
	lint := service.NewWikiLintService(wiki, kb, nil)
	h := handler.NewWikiPageHandler(wiki, kb, lint, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/knowledgebase/:kb_id/wiki/auto-fix", func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		h.AutoFix(c)
	})
	return db, repo, lint, router
}

func failLintMetadata(t *testing.T, db *gorm.DB) {
	t.Helper()
	// The body fix must commit, while the subsequent metadata-only update
	// actually fails inside SQLite. There is no mocked successful repository.
	require.NoError(t, db.Exec(`CREATE TRIGGER lint_metadata_failure BEFORE UPDATE ON wiki_pages
 WHEN NEW.content = OLD.content
 BEGIN SELECT RAISE(ABORT, 'WIKI_LINT_SQL_TOKEN_CANARY'); END`).Error)
}

func TestWikiLintAutoFixRealSQLPartialFailure(t *testing.T) {
	db, repo, lint, _ := newLintSQLFixture(t)
	failLintMetadata(t, db)
	var logs bytes.Buffer
	sink := logrus.New()
	sink.SetOutput(&logs)
	ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(sink))
	fixed, err := lint.AutoFix(ctx, lintKB)
	require.Equal(t, 1, fixed)
	var partial *service.WikiAutoFixIncompleteError
	require.ErrorAs(t, err, &partial)
	require.Equal(t, fixed, partial.AppliedFixes)
	require.NotContains(t, err.Error(), "WIKI_LINT_SQL_TOKEN_CANARY")
	require.NotContains(t, logs.String(), "WIKI_LINT_SQL_TOKEN_CANARY")
	require.Contains(t, logs.String(), "applied_fixes=1")
	page, getErr := repo.GetBySlug(ctx, lintKB, "concept/lint")
	require.NoError(t, getErr)
	require.NotContains(t, page.Content, "[[missing-target]]", "committed body fix is not rolled back")
	require.Equal(t, 1, page.Version, "existing auto-link update does not bump the version")
	require.NoError(t, db.Exec("DROP TRIGGER lint_metadata_failure").Error)
	_, err = lint.AutoFix(ctx, lintKB)
	require.NoError(t, err, "explicit retry after SQL recovery")
	t.Log("real SQLite metadata ABORT: fixed=1 retained, controlled incomplete error, explicit retry succeeds")
}

func TestWikiLintAutoFixRealHTTPPartialFailure(t *testing.T) {
	db, repo, _, router := newLintSQLFixture(t)
	failLintMetadata(t, db)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/knowledgebase/"+lintKB+"/wiki/auto-fix", nil))
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "1 fixes applied")
	require.Contains(t, response.Body.String(), "incomplete")
	require.NotContains(t, response.Body.String(), "WIKI_LINT_SQL_TOKEN_CANARY")
	require.NotContains(t, response.Body.String(), "Auto-fixed")
	page, err := repo.GetBySlug(context.Background(), lintKB, "concept/lint")
	require.NoError(t, err)
	require.NotContains(t, page.Content, "[[missing-target]]")
	t.Logf("real AutoFix handler HTTP=%d; applied body fix retained; raw SQL error withheld", response.Code)
}

func TestWikiLintAutoFixRealHTTPNormalSuccess(t *testing.T) {
	_, repo, lint, router := newLintSQLFixture(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/knowledgebase/"+lintKB+"/wiki/auto-fix", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"fixed":1`)
	page, err := repo.GetBySlug(context.Background(), lintKB, "concept/lint")
	require.NoError(t, err)
	require.Equal(t, 1, page.Version)
	require.Empty(t, page.OutLinks)
	fixed, err := lint.AutoFix(context.Background(), lintKB)
	require.NoError(t, err)
	require.Zero(t, fixed, "successful retry without new findings is a no-op")
}

func TestWikiLintIncompleteErrorPreservesCancellationWithoutExposingCause(t *testing.T) {
	// The real SQL tests above establish propagation. This additional type
	// check guards controlled messages and cancellation classification.
	_, _, lint, _ := newLintSQLFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lint.AutoFix(ctx, lintKB)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}
