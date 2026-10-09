package middleware_test

// Characterization of the existing contract, not the proposed governance fix.
// Real endpoint handlers/service/repository, MCPEndpointAuth, MCP transport and
// ResolveKB run against isolated SQLite. Web identity, sharing and index are
// explicit synthetic adapters; this is not PostgreSQL/JWT/vectorstore acceptance.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

type lifecycleUser struct {
	ID     string `gorm:"primaryKey"`
	Active bool
}
type lifecycleMember struct {
	UserID   string `gorm:"primaryKey"`
	TenantID uint64 `gorm:"primaryKey"`
	Role     string
	Present  bool
}
type lifecycleGrant struct {
	TenantID uint64 `gorm:"primaryKey"`
	KBID     string `gorm:"primaryKey"`
	Role     string
}
type lifecycleAdapters struct {
	interfaces.KnowledgeBaseService
	db       *gorm.DB
	kbs      map[string]*types.KnowledgeBase
	mu       sync.Mutex
	searched []string
}

func (a *lifecycleAdapters) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	kb := a.kbs[id]
	if kb == nil {
		return nil, fmt.Errorf("synthetic missing KB")
	}
	return kb, nil
}
func (a *lifecycleAdapters) GetKnowledgeBasesByIDsOnly(_ context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	rows := []*types.KnowledgeBase{}
	for _, id := range ids {
		if kb := a.kbs[id]; kb != nil {
			rows = append(rows, kb)
		}
	}
	return rows, nil
}
func (a *lifecycleAdapters) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	rows := []*types.KnowledgeBase{}
	for _, kb := range a.kbs {
		if kb.TenantID == types.MustTenantIDFromContext(ctx) {
			rows = append(rows, kb)
		}
	}
	return rows, nil
}
func (a *lifecycleAdapters) HybridSearch(ctx context.Context, id string, p types.SearchParams) ([]*types.SearchResult, error) {
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != 1 {
		return nil, fmt.Errorf("synthetic index detected wrong caller")
	}
	a.mu.Lock()
	a.searched = append(a.searched, id)
	a.mu.Unlock()
	return []*types.SearchResult{}, nil
}
func (a *lifecycleAdapters) count() int { a.mu.Lock(); defer a.mu.Unlock(); return len(a.searched) }

func (a *lifecycleAdapters) ResolveEmbeddingModelKeys(_ context.Context, _ []*types.KnowledgeBase) map[string]string {
	return map[string]string{}
}

type lifecycleShares struct {
	interfaces.KBShareService
	db *gorm.DB
}

func (a *lifecycleShares) CheckTenantKBPermission(ctx context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	var row lifecycleGrant
	err := a.db.WithContext(ctx).Where("tenant_id = ? AND kb_id = ?", tenant, id).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", false, nil
	}
	return types.OrgMemberRole(row.Role), err == nil, err
}

type lifecycleTenants struct{ interfaces.TenantService }

func (*lifecycleTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: id, Status: "active", Name: fmt.Sprintf("Synthetic %d", id)}, nil
}

type lifecycleHarness struct {
	db         *gorm.DB
	server     *httptest.Server
	svc        interfaces.MCPEndpointService
	adapters   *lifecycleAdapters
	id, token  string
	mu         sync.Mutex
	principals []types.Caller
}

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "isolated-lifecycle.sqlite")), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&types.MCPEndpoint{}, &lifecycleUser{}, &lifecycleMember{}, &lifecycleGrant{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []lifecycleUser{{"zhang", true}, {"li", true}, {"wang", true}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []lifecycleMember{{"zhang", 1, "admin", true}, {"li", 1, "viewer", true}, {"wang", 1, "admin", true}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&lifecycleGrant{1, "KB-S", "viewer"}).Error; err != nil {
		t.Fatal(err)
	}
	a := &lifecycleAdapters{db: db, kbs: map[string]*types.KnowledgeBase{}}
	for id, tenant := range map[string]uint64{"KB-A": 1, "KB-S": 2, "KB-B": 2} {
		a.kbs[id] = &types.KnowledgeBase{ID: id, Name: id, TenantID: tenant, Type: "document", IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}}
	}
	shares := &lifecycleShares{db: db}
	tenants := &lifecycleTenants{}
	repo := repository.NewMCPEndpointRepository(db)
	svc := service.NewMCPEndpointService(repo, a, shares, nil)
	h := &lifecycleHarness{db: db, svc: svc, adapters: a}
	r := gin.New()
	r.ContextWithFallback = true
	// Synthetic sign-in adapter loads live account/membership state. Mutation role
	// enforcement and endpoint handlers are production code, not permissive mocks.
	web := func(c *gin.Context) {
		var u lifecycleUser
		var m lifecycleMember
		uid := c.GetHeader("X-Synthetic-User")
		if db.First(&u, "id = ?", uid).Error != nil || !u.Active || db.First(&m, "user_id = ? AND tenant_id = ?", uid, 1).Error != nil || !m.Present {
			c.AbortWithStatus(401)
			return
		}
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 1, UserID: uid, Role: types.TenantRole(m.Role)})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
	yes := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &yes}}
	guard := middleware.RequireRole(types.TenantRoleAdmin, cfg)
	management := handler.NewMCPEndpointHandler(svc)
	r.POST("/api/v1/mcp-endpoints", web, guard, management.CreateMCPEndpoint)
	r.DELETE("/api/v1/mcp-endpoints/:endpoint_id", web, guard, management.DeleteMCPEndpoint)
	r.POST("/api/v1/mcp-endpoints/:endpoint_id/rotate-token", web, guard, management.RotateMCPEndpointToken)
	srv := mcpserver.NewServer(a, nil, nil, nil, nil, nil, nil, shares, tenants, repo, db, nil, nil, nil, nil, nil)
	capture := func(c *gin.Context) {
		h.mu.Lock()
		h.principals = append(h.principals, types.CallerFromContext(c.Request.Context()))
		h.mu.Unlock()
		c.Next()
	}
	r.POST("/mcp/:endpoint_id", middleware.MCPEndpointAuth(svc, tenants), capture, gin.WrapH(srv.Handler()))
	h.server = httptest.NewServer(r)
	t.Cleanup(h.server.Close)
	status, body := h.request(t, "POST", "/api/v1/mcp-endpoints", "zhang", "", map[string]any{"name": "Synthetic machine", "enabled": true, "knowledge_base_ids": []string{"KB-A", "KB-S"}, "tools": []string{"search_knowledge"}})
	if status != 201 {
		t.Fatalf("create status %d", status)
	}
	var reply struct {
		Data struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	h.id, h.token = reply.Data.ID, reply.Data.Token
	if h.id == "" || h.token == "" {
		t.Fatal("missing test credential")
	}
	return h
}
func (h *lifecycleHarness) request(t *testing.T, method, path, user, token string, payload any) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if user != "" {
		req.Header.Set("X-Synthetic-User", user)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body.Bytes()
}
func (h *lifecycleHarness) search(t *testing.T, token, kb string) (int, bool) {
	t.Helper()
	status, body := h.request(t, "POST", "/mcp/"+h.id, "", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "search_knowledge", "arguments": map[string]any{"query": "synthetic probe", "mode": "keyword", "knowledge_base_ids": []string{kb}}}})
	if status != 200 {
		return status, false
	}
	raw := strings.TrimSpace(string(body))
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data:") {
			raw = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			break
		}
	}
	var reply struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	return status, len(reply.Error) == 0 && !reply.Result.IsError
}
func TestMCPEndpointLifecycleHTTPCharacterization(t *testing.T) {
	for _, scenario := range []string{"creator-demoted", "creator-removed", "creator-disabled", "share-revoked", "cross-tenant", "admin-revokes", "admin-rotates"} {
		t.Run(scenario, func(t *testing.T) {
			h := newLifecycleHarness(t)
			before := h.adapters.count()
			status, allowed := h.search(t, h.token, "KB-A")
			if status != 200 || !allowed || h.adapters.count() <= before {
				t.Fatalf("authorized search never executed: status=%d allowed=%v", status, allowed)
			}
			switch scenario {
			case "creator-demoted", "creator-removed", "creator-disabled":
				var update *gorm.DB
				if scenario == "creator-demoted" {
					update = h.db.Model(&lifecycleMember{}).Where("user_id = ?", "zhang").Update("role", "viewer")
				}
				if scenario == "creator-removed" {
					update = h.db.Model(&lifecycleMember{}).Where("user_id = ?", "zhang").Update("present", false)
				}
				if scenario == "creator-disabled" {
					update = h.db.Model(&lifecycleUser{}).Where("id = ?", "zhang").Update("active", false)
				}
				if update.Error != nil {
					t.Fatal(update.Error)
				}
				code, _ := h.request(t, "POST", "/api/v1/mcp-endpoints", "zhang", "", map[string]any{"name": "Should not create"})
				if code != 401 && code != 403 {
					t.Fatalf("changed web identity still creates endpoint: %d", code)
				}
				// Existing machine contract: characterize retention, do not call it a fix.
				before = h.adapters.count()
				status, allowed = h.search(t, h.token, "KB-A")
				if status != 200 || !allowed || h.adapters.count() <= before {
					t.Fatalf("unexpected existing machine behavior: %d %v", status, allowed)
				}
				t.Logf("scenario=%s management=%d MCP=%d KB-authorized=%v; machine identity survives creator state", scenario, code, status, allowed)
			case "share-revoked":
				if status, allowed = h.search(t, h.token, "KB-S"); status != 200 || !allowed {
					t.Fatal("initial share not searchable")
				}
				if err := h.db.Where("tenant_id = ? AND kb_id = ?", 1, "KB-S").Delete(&lifecycleGrant{}).Error; err != nil {
					t.Fatal(err)
				}
				before = h.adapters.count()
				status, allowed = h.search(t, h.token, "KB-S")
				if status != 200 || allowed || h.adapters.count() != before {
					t.Fatalf("revoked share reached index: %d %v", status, allowed)
				}
				t.Log("token authenticated; MCP KB error refused revoked share before index")
			case "cross-tenant":
				before = h.adapters.count()
				status, allowed = h.search(t, h.token, "KB-B")
				if status != 200 || allowed || h.adapters.count() != before {
					t.Fatalf("foreign KB reached index: %d %v", status, allowed)
				}
			case "admin-revokes":
				code, _ := h.request(t, "DELETE", "/api/v1/mcp-endpoints/"+h.id, "li", "", nil)
				if code != 403 {
					t.Fatalf("viewer revoked endpoint: %d", code)
				}
				code, _ = h.request(t, "DELETE", "/api/v1/mcp-endpoints/"+h.id, "wang", "", nil)
				if code != 200 {
					t.Fatalf("other administrator revoke: %d", code)
				}
				for i := 0; i < 4; i++ {
					if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
						t.Fatalf("completed revoke still authenticates: %d", status)
					}
				}
			case "admin-rotates":
				code, body := h.request(t, "POST", "/api/v1/mcp-endpoints/"+h.id+"/rotate-token", "wang", "", nil)
				if code != 200 {
					t.Fatalf("rotate: %d", code)
				}
				var reply struct {
					Data struct {
						Token string `json:"token"`
					}
				}
				if err := json.Unmarshal(body, &reply); err != nil {
					t.Fatal(err)
				}
				if status, _ := h.search(t, h.token, "KB-A"); status != 401 {
					t.Fatal("old key survived rotation")
				}
				if status, allowed := h.search(t, reply.Data.Token, "KB-A"); status != 200 || !allowed {
					t.Fatal("rotated key unusable")
				}
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, caller := range h.principals {
				if caller.TenantID != 1 || caller.UserID != "mcp-"+h.id {
					t.Fatalf("wrong machine caller: %+v", caller)
				}
			}
		})
	}
}
