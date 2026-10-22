package router

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Adapters delegate the authentication lookups to the real repositories. No
// request headers or fixture context constructors supply the HTTP principal.
type documentTestTenants struct {
	interfaces.TenantService
	repo interfaces.TenantRepository
}

func (s documentTestTenants) GetTenantByID(ctx context.Context, id uint64) (*types.Tenant, error) {
	return s.repo.GetTenantByID(ctx, id)
}

type documentTestUsers struct {
	interfaces.UserService
	repo interfaces.UserRepository
}

func (s documentTestUsers) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	return s.repo.GetUserByID(ctx, id)
}
func (s documentTestUsers) GetUserByTenantID(ctx context.Context, id uint64) (*types.User, error) {
	return s.repo.GetUserByTenantID(ctx, id)
}

type documentTestMembers struct {
	interfaces.TenantMemberService
	repo interfaces.TenantMemberRepository
}

func (s documentTestMembers) GetMembership(ctx context.Context, id string, tenant uint64) (*types.TenantMember, error) {
	return s.repo.Get(ctx, id, tenant)
}

type documentHTTPFixture struct {
	f                *fixture.Fixture
	engine           *gin.Engine
	keys             map[uint64]string
	revokeAfter      atomic.Bool
	uncertifiedCalls atomic.Int64
}

func newDocumentHTTPFixture(t *testing.T) *documentHTTPFixture {
	t.Helper()
	f := fixture.New(t)
	h := &documentHTTPFixture{f: f, keys: map[uint64]string{}}
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_api_keys(id,tenant_id,scope_type,name,key_hash,api_key,capabilities,full_access) VALUES(9,99,'tenant','synthetic-E','synthetic-hash-9','not-a-usable-key-9','[]',true)").Error)
	for _, id := range []uint64{1, 2, 3, 4, 6, 7, 8, 9} {
		var random [32]byte
		_, err := rand.Read(random[:])
		require.NoError(t, err)
		token := "mcp_" + hex.EncodeToString(random[:])
		digest := sha256.Sum256([]byte(token))
		require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET key_hash=? WHERE id=?", hex.EncodeToString(digest[:]), id).Error)
		h.keys[id] = token // Never log these transient synthetic credentials.
	}
	kr := repository.NewKnowledgeRepository(f.DB)
	cr := repository.NewChunkRepository(f.DB)
	kbr := repository.NewKnowledgeBaseRepository(f.DB)
	cs := service.NewChunkService(cr, kr, kbr, nil, nil, nil, nil, nil)
	ks, err := service.NewKnowledgeService(nil, kr, nil, nil, nil, nil, cs, cr, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	policy := service.NewDocumentReadPolicy(true, f.Reader)
	ks, err = service.ConfigureKnowledgeDocumentReads(ks, policy)
	require.NoError(t, err)
	cs, err = service.ConfigureChunkDocumentReads(cs, policy)
	require.NoError(t, err)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}}
	kh := handler.NewKnowledgeHandler(cfg, ks, nil, nil, nil, cs, nil, nil, nil, nil, nil)
	ch := handler.NewChunkHandler(cs, ks)
	g := newRBACGuards(cfg, nil, nil, kh, ch, nil, nil, ks, cs, nil, nil)
	boundary := middleware.NewDocumentCoreReadBoundary(ks, cs)
	e := gin.New()
	e.Use(middleware.ErrorHandler(), boundary.Outer())
	uncertified := func(c *gin.Context) { h.uncertifiedCalls.Add(1); c.JSON(200, gin.H{"unsafe": fixture.Canary}) }
	// Match the real router ordering: these capability/IM routes precede Auth.
	e.GET("/r/:token", uncertified)
	e.GET("/api/v1/files/presigned", uncertified)
	e.POST("/api/v1/im/callback/:channel_id", uncertified)
	e.Use(middleware.Auth(documentTestTenants{repo: repository.NewTenantRepository(f.DB)}, documentTestUsers{repo: repository.NewUserRepository(f.DB)}, documentTestMembers{repo: repository.NewTenantMemberRepository(f.DB)}, service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(f.DB)), cfg))
	e.Use(boundary.Inner())
	// Registered outside the per-route capability gate, but behind actual Auth.
	e.GET("/api/v1/auth/me", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "data": gin.H{"user": gin.H{"id": types.CallerFromContext(c.Request.Context()).UserID}}})
	})
	e.Use(g.apiKeyAuthorizer.Middleware())
	e.Use(func(c *gin.Context) {
		c.Next()
		if h.revokeAfter.Swap(false) {
			f.Subject(t, "sensitive", "D", "revoked", 0)
		}
	})
	r := e.Group("/api/v1")
	RegisterKnowledgeRoutes(r, kh, g)
	RegisterChunkRoutes(r, ch, g)
	// Uncertified output paths must be held before invoking even a local spy.
	for _, path := range []string{"/knowledge-bases/:id/hybrid-search", "/sessions/:id/chat", "/agent-chat/:id", "/files/:id", "/knowledge-bases/:id/wiki", "/artifacts"} {
		r.GET(path, uncertified)
	}
	h.engine = e
	return h
}
func (h *documentHTTPFixture) get(path string, key uint64, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if key != 0 {
		r.Header.Set("X-API-Key", h.keys[key])
		tenant := uint64(99)
		if key == 1 || key == 3 {
			tenant = 1
		}
		r.Header.Set("X-Tenant-ID", strconv.FormatUint(tenant, 10))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.engine.ServeHTTP(w, r)
	return w
}

func TestDocumentM5RealKnowledgeChunkRoutes(t *testing.T) {
	h := newDocumentHTTPFixture(t)
	for _, path := range []string{"/api/v1/knowledge/public-doc", "/api/v1/knowledge-bases/public-kb/knowledge", "/api/v1/knowledge/batch?ids=public-doc&ids=denied-doc&ids=sensitive", "/api/v1/chunks/public-doc", "/api/v1/chunks/by-id/public-doc-chunk"} {
		w := h.get(path, 1, nil)
		require.Equal(t, 200, w.Code, path+" "+w.Body.String())
		require.Contains(t, w.Body.String(), fixture.PublicText)
		require.NotContains(t, w.Body.String(), fixture.Canary)
		require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
	}
	for _, tc := range []struct {
		path string
		key  uint64
	}{
		{"/api/v1/knowledge/denied-doc", 1}, {"/api/v1/knowledge/unknown-doc", 1}, {"/api/v1/knowledge/sensitive", 1},
		{"/api/v1/chunks/by-id/sensitive-chunk", 2}, {"/api/v1/chunks/sensitive", 2}, {"/api/v1/knowledge/sensitive", 7},
		{"/api/v1/knowledge/public-doc", 3}, {"/api/v1/knowledge/public-doc", 8}, {"/api/v1/knowledge/public-doc", 0},
		{"/api/v1/knowledge/blocked", 4}, {"/api/v1/chunks/blocked", 4}, {"/api/v1/chunks/sensitive/sensitive-chunk/revisions", 4},
		{"/api/v1/knowledge/blocked", 9}, {"/api/v1/knowledge/sensitive", 9},
	} {
		w := h.get(tc.path, tc.key, map[string]string{"X-From-MCP": "false", "User-Agent": "trusted-web", "X-User-ID": "D", "X-Enterprise-Grant": "public"})
		require.Equal(t, 403, w.Code, tc.path+" "+w.Body.String())
		require.NotContains(t, w.Body.String(), fixture.Canary)
	}
	w := h.get("/api/v1/knowledge-bases/private-kb/knowledge", 2, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"total":0`)
	require.NotContains(t, w.Body.String(), fixture.Canary)
	w = h.get("/api/v1/knowledge-bases/public-kb/knowledge?keyword="+fixture.Canary, 1, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"total":0`)
	for _, path := range []string{"/api/v1/knowledge/sensitive", "/api/v1/chunks/by-id/sensitive-chunk"} {
		w = h.get(path, 4, nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), fixture.Canary)
	}
	w = h.get("/api/v1/knowledge/public-doc", 1, map[string]string{"X-Tenant-ID": "99"})
	require.Equal(t, 403, w.Code, "public grant cannot select target workspace")
	var membership int64
	require.NoError(t, h.f.DB.Table("tenant_members").Where("user_id='A' AND tenant_id=99").Count(&membership).Error)
	require.Zero(t, membership)
	for _, path := range []string{"/api/v1/knowledge-bases/public-kb/hybrid-search", "/api/v1/sessions/test/chat", "/api/v1/agent-chat/test", "/api/v1/files/test", "/api/v1/knowledge-bases/public-kb/wiki"} {
		w = h.get(path, 4, nil)
		require.Equal(t, 403, w.Code)
		require.NotContains(t, w.Body.String(), fixture.Canary)
	}
	require.Zero(t, h.uncertifiedCalls.Load(), "HOLD must reject before downstream content/model dispatch")
	for _, path := range []string{"/r/historical-bearer", "/api/v1/files/presigned", "/api/v1/artifacts"} {
		w = h.get(path, 0, nil)
		require.Equal(t, 403, w.Code)
		require.NotContains(t, w.Body.String(), fixture.Canary)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/im/callback/synthetic", nil)
	w = httptest.NewRecorder()
	h.engine.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
	require.Zero(t, h.uncertifiedCalls.Load(), "anonymous capability routes must be held before Auth too")
}

func TestDocumentM5HTTPRevocationBeforeFlushAndKeyRevocation(t *testing.T) {
	h := newDocumentHTTPFixture(t)
	h.revokeAfter.Store(true)
	w := h.get("/api/v1/chunks/by-id/sensitive-chunk", 4, nil)
	require.Equal(t, 403, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), fixture.Canary, "buffered content is discarded after concurrent revoke")
	w = h.get("/api/v1/knowledge/sensitive", 4, nil)
	require.Equal(t, 403, w.Code)
	w = h.get("/api/v1/auth/me", 4, nil)
	require.Equal(t, 200, w.Code)
	require.NoError(t, h.f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=4").Error)
	w = h.get("/api/v1/auth/me", 4, nil)
	require.Equal(t, 401, w.Code)
	w = h.get("/api/v1/knowledge/public-doc", 4, nil)
	require.Equal(t, 403, w.Code)
	require.NotContains(t, w.Body.String(), fixture.PublicText)
}

func TestDocumentM5HTTPPortalUnpublishUsesLatestState(t *testing.T) {
	h := newDocumentHTTPFixture(t)
	w := h.get("/api/v1/knowledge/public-doc", 1, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, h.f.DB.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
	for _, path := range []string{"/api/v1/knowledge/public-doc", "/api/v1/chunks/public-doc"} {
		w = h.get(path, 1, nil)
		require.Equal(t, 403, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), fixture.PublicText)
	}
	w = h.get("/api/v1/knowledge-bases/public-kb/knowledge", 1, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"total":0`)
	w = h.get("/api/v1/knowledge/sensitive", 4, nil)
	require.Equal(t, 200, w.Code, "private explicit grants do not become public policy")
}
