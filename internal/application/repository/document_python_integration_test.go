//go:build sqlite_fts5 && knowhub_m3_integration

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/access/httpboundary"
	retrieversqlite "github.com/Tencent/WeKnora/internal/application/repository/retriever/sqlite"
	"github.com/Tencent/WeKnora/internal/types"
	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This uses real TCP, the isolated candidate Python module and the real SQLite driver.
// It certifies the isolated adapter, not existing application's rich handlers.
// The build tag is enabled by the dedicated integration runner; no assertion
// is skipped for a missing Python/MCP dependency.
func TestDocumentM3PythonHTTPCompatibility(t *testing.T) {
	documentSQLiteVecOnce.Do(sqlitevec.Auto)
	f := newEnterpriseFixture(t)
	// A persisted machine FullAccess Key, not a user Key renamed in headers.
	require.NoError(t, f.db.Exec(`INSERT INTO tenant_api_keys VALUES(5,NULL,'tenant','[]',NULL,NULL,true)`).Error)
	for _, id := range []string{"sensitive", "blocked", "not-approved"} {
		require.NoError(t, f.db.Exec(`INSERT INTO knowledges SELECT ?,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at,deleted_at FROM knowledges WHERE id='doc'`, id).Error)
		require.NoError(t, f.db.Exec(`INSERT INTO chunks SELECT ?,tenant_id,knowledge_base_id,?,'SECRET_M3_CANARY',1,true,'text',1,updated_at,deleted_at FROM chunks WHERE id='chunk'`, id+"-chunk", id).Error)
	}
	f.approveClean(t)
	f.documentPolicy(t, "sensitive", "restricted")
	f.documentPolicy(t, "blocked", "mcp_blocked")
	snapshot, err := f.repo.LoadDocumentReadSnapshot(context.Background(), "B", 0, "public-kb")
	require.NoError(t, err)
	var version string
	for _, d := range snapshot.Documents {
		if d.ID == "sensitive" {
			version = d.CurrentVersion
		}
	}
	_, err = access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), f.repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "sensitive", UserID: "B", Operation: types.DocumentRead, State: "approved", ResourceVersion: version}, Reason: "synthetic Python compatibility grant"})
	require.NoError(t, err)
	r := f.documentReader()
	engine := retrieversqlite.NewSQLiteRetrieveEngineRepository(f.db)
	for _, row := range []struct{ id, doc, body string }{{"chunk", "doc", "Synthetic approved knowledge: ERR_M2_42"}, {"sensitive-chunk", "sensitive", "SECRET_M3_CANARY"}} {
		require.NoError(t, engine.Save(context.Background(), &types.IndexInfo{SourceID: row.id, ChunkID: row.id, KnowledgeID: row.doc, KnowledgeBaseID: "public-kb", SourceType: types.ChunkSourceType, Content: row.body, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{row.id: []float32{1, 0, 0}}}))
	}
	var nonceBytes [24]byte
	_, err = rand.Read(nonceBytes[:])
	require.NoError(t, err)
	nonce := hex.EncodeToString(nonceBytes[:])
	validate := func(body []byte) error {
		if !json.Valid(body) || strings.Contains(string(body), "https://") || strings.Contains(string(body), "/files/") {
			return access.ErrForbidden
		}
		return nil
	}
	readRoute := func(parameter string, op types.DocumentOperation) httpboundary.DocumentReadRoute {
		return httpboundary.DocumentReadRoute{
			Authorize: func(c *gin.Context) (func() error, error) {
				doc := c.Param(parameter)
				if parameter == "" {
					doc = "doc"
				}
				g, err := r.Authorize(c.Request.Context(), "public-kb", doc, op)
				if err != nil {
					return nil, err
				}
				c.Set("m3.fixture.grant", g)
				return func() error { return g.Revalidate(c.Request.Context()) }, nil
			}, ValidateResponse: validate,
		}
	}
	noopRoute := httpboundary.DocumentReadRoute{Authorize: func(c *gin.Context) (func() error, error) {
		if _, ok := c.Get("m3.fixture.authenticated"); !ok {
			return nil, access.ErrUnauthorized
		}
		return func() error { return nil }, nil
	}, ValidateResponse: validate}
	controlRoute := httpboundary.DocumentReadRoute{Authorize: func(c *gin.Context) (func() error, error) {
		if c.GetHeader("X-Test-Control") != nonce {
			return nil, access.ErrForbidden
		}
		return func() error { return nil }, nil
	}, ValidateResponse: validate}
	boundary := httpboundary.NewDocumentReadBoundary(true, map[string]httpboundary.DocumentReadRoute{
		"GET /api/v1/auth/me":                            noopRoute,
		"GET /api/v1/knowledge/:id":                      readRoute("id", types.DocumentRead),
		"GET /api/v1/chunks/:knowledge_id":               readRoute("knowledge_id", types.DocumentRead),
		"GET /api/v1/knowledge-bases":                    readRoute("", types.DocumentSearch),
		"POST /api/v1/knowledge-bases/:id/hybrid-search": readRoute("", types.DocumentSearch),
		"POST /__test/control":                           controlRoute,
	})
	var seenMu sync.Mutex
	seen := map[string]int{}
	deniedB := 0
	app := gin.New()
	app.Use(func(c *gin.Context) {
		seenMu.Lock()
		seen[c.Request.Method+" "+c.Request.URL.Path]++
		seenMu.Unlock()
		c.Next()
	})
	app.Use(boundary.Outer())
	app.GET("/files/presigned", func(c *gin.Context) { c.String(200, "SECRET_M3_CANARY") })
	app.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/__test/control" {
			c.Next()
			return
		}
		key := c.GetHeader("X-API-Key")
		var user string
		var id uint64
		switch key {
		case "m3-A":
			user, id = "A", 1
		case "m3-B":
			user, id = "B", 4
		case "m3-old":
			user, id = "A", 3
		case "m3-full":
			user, id = "", 5
		default:
			c.AbortWithStatus(401)
			return
		}
		var n int64
		if err := f.db.Table("tenant_api_keys").Where("id=? AND revoked_at IS NULL", id).Count(&n).Error; err != nil || n != 1 {
			if key == "m3-B" && (c.Request.URL.Path == "/api/v1/auth/me" || c.Request.URL.Path == "/api/v1/knowledge/doc") {
				seenMu.Lock()
				deniedB++
				seenMu.Unlock()
			}
			c.AbortWithStatus(401)
			return
		}
		ctx := documentCtx(user, id)
		if key == "m3-full" {
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, Role: types.TenantRoleViewer})
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 5, ScopeType: types.APIKeyScopeTenant, FullAccess: true})
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "synthetic-machine"})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Set("m3.fixture.authenticated", true)
		c.Next()
	})
	app.Use(boundary.Inner())
	app.GET("/api/v1/auth/me", func(c *gin.Context) { c.JSON(200, gin.H{"data": gin.H{"id": "synthetic"}}) })
	readHandler := func(c *gin.Context) {
		v, _ := c.Get("m3.fixture.grant")
		g := v.(*access.DocumentReadGrant)
		rows, err := r.ReadText(c.Request.Context(), g, NewChunkRepository(f.db))
		if err != nil {
			c.AbortWithStatus(403)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": rows})
	}
	app.GET("/api/v1/knowledge/:id", readHandler)
	app.GET("/api/v1/chunks/:knowledge_id", readHandler)
	app.GET("/api/v1/knowledge-bases", func(c *gin.Context) { c.JSON(200, gin.H{"data": []gin.H{{"id": "public-kb", "name": "public-kb"}}}) })
	app.POST("/api/v1/knowledge-bases/:id/hybrid-search", func(c *gin.Context) {
		if c.Param("id") != "public-kb" {
			c.AbortWithStatus(403)
			return
		}
		var query struct {
			QueryText string `json:"query_text"`
		}
		if c.ShouldBindJSON(&query) != nil {
			c.AbortWithStatus(400)
			return
		}
		v, _ := c.Get("m3.fixture.grant")
		g := v.(*access.DocumentReadGrant)
		rows, err := access.SearchDocuments(types.WithExecutionTenant(c.Request.Context(), 99), []*access.DocumentReadGrant{g}, engine, types.RetrieveParams{Query: query.QueryText, Embedding: []float32{1, 0, 0}, TopK: 5})
		if err != nil {
			c.AbortWithStatus(403)
			return
		}
		c.JSON(200, gin.H{"data": rows})
	})
	app.POST("/__test/control", func(c *gin.Context) {
		var input struct{ Action string }
		if c.ShouldBindJSON(&input) != nil {
			c.AbortWithStatus(400)
			return
		}
		var err error
		switch input.Action {
		case "status":
		case "reset":
			err = f.db.Exec(`UPDATE tenant_api_keys SET revoked_at=NULL WHERE id=4`).Error
			if err == nil {
				err = f.db.Exec(`UPDATE document_subject_grants SET state='approved',revision=revision+1 WHERE knowledge_id='sensitive' AND user_id='B'`).Error
			}
		case "revoke-grant":
			err = f.db.Exec(`UPDATE document_subject_grants SET state='revoked',revision=revision+1 WHERE knowledge_id='sensitive' AND user_id='B'`).Error
		case "revoke-key":
			err = f.db.Exec(`UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=4`).Error
		default:
			c.AbortWithStatus(400)
			return
		}
		if err != nil {
			c.AbortWithStatus(500)
			return
		}
		seenMu.Lock()
		count := deniedB
		knowledgeCalls := seen["GET /api/v1/knowledge/doc"]
		seenMu.Unlock()
		c.JSON(200, gin.H{"ok": true, "denied_b": count, "knowledge_calls": knowledgeCalls})
	})
	server := httptest.NewServer(app)
	defer server.Close()
	for _, path := range []string{"/files/presigned", "/api/v1/knowledge/doc/preview", "/api/v1/chunks/secret-chunk", "/api/v1/knowledge/batch"} {
		req, err := http.NewRequest("GET", server.URL+path, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", "m3-A")
		req.Header.Set("X-From-MCP", "false")
		req.Header.Set("X-Tenant-ID", "999")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, 403, resp.StatusCode)
		resp.Body.Close()
	}
	python, err := exec.LookPath("python")
	require.NoError(t, err)
	script, err := filepath.Abs("../../../docs/integration/knowhub-mcp-portal/tools/batch4_python_http.py")
	require.NoError(t, err)
	cmd := exec.Command(python, script)
	cmd.Env = append(os.Environ(), "KNOWHUB_M3_BACKEND="+server.URL, "KNOWHUB_M3_CONTROL="+nonce)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(string(output))
	seenMu.Lock()
	defer seenMu.Unlock()
	for path, n := range seen {
		t.Log(fmt.Sprintf("REST %s calls=%d", path, n))
	}
}
