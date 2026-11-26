package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func persistentFolderHTTP(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "folders.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases "+
		"(id TEXT PRIMARY KEY, tenant_id INTEGER, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id,tenant_id) VALUES ('kb-1',1)").Error)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges (
		id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT,
		folder_path TEXT NOT NULL DEFAULT '', parse_status TEXT,
		deleted_at DATETIME, updated_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeFolder{}))
	repo := repository.NewKnowledgeRepository(db)
	kb := &stubFolderKBService{}
	svc, err := service.NewKnowledgeService(
		nil, repo, nil, kb, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	require.NoError(t, err)
	cfg := &config.Config{}
	h := &KnowledgeHandler{kbService: kb, kgService: svc, cfg: cfg}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "fixture-owner")
		role := types.TenantRoleOwner
		if c.GetHeader("X-Fixture-Viewer") == "true" {
			role = types.TenantRoleViewer
		}
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "fixture-owner")
		c.Next()
	})
	p := "/api/v1/knowledge-bases/:id/knowledge/folders"
	r.GET(p, h.ListKnowledgeFolders)
	r.POST(p, middleware.RequireRole(types.TenantRoleContributor, cfg), h.CreateKnowledgeFolder)
	r.PUT(p, middleware.RequireRole(types.TenantRoleContributor, cfg), h.RenameKnowledgeFolder)
	r.DELETE(p+"/empty", middleware.RequireRole(types.TenantRoleContributor, cfg), h.DeleteKnowledgeFolder)
	return r
}

func TestPersistentFolderHTTP(t *testing.T) {
	r := persistentFolderHTTP(t)
	p := "/api/v1/knowledge-bases/kb-1/knowledge/folders"
	call := func(method, path, body string, viewer bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if viewer {
			req.Header.Set("X-Fixture-Viewer", "true")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, 403, call("POST", p, `{"name":"denied"}`, true).Code)
	for _, body := range []string{
		`{"name":"empty"}`,
		`{"parent_path":"empty","name":"child"}`,
		`{"parent_path":"empty/child","name":"deep"}`,
	} {
		w := call("POST", p, body, false)
		require.Equal(t, 201, w.Code, w.Body.String())
	}
	w := call("GET", p, "", false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "empty/child/deep")
	require.Equal(t, 409, call("DELETE", p+"/empty?path=empty", "", false).Code)
	w = call("PUT", p, `{"from":"empty","to":"renamed"}`, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"moved_count":0`)
	require.Equal(t, 403, call("DELETE", p+"/empty?path=renamed/child/deep", "", true).Code)
	require.Equal(t, 200, call("DELETE", p+"/empty?path=renamed/child/deep", "", false).Code)
}

// Opt-in loopback fixture uses actual handlers/services/repositories and a real
// disk SQLite DB. Fixture identity injection exists only in this _test.go file.
func TestKnowledgeFolderBrowserServer(t *testing.T) {
	if os.Getenv("FOLDER_BROWSER_TEST") != "1" {
		t.Skip("opt-in browser fixture")
	}
	srv := &http.Server{Addr: "127.0.0.1:18088", Handler: persistentFolderHTTP(t), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { _ = srv.Close() })
	require.NoError(t, srv.ListenAndServe())
}
