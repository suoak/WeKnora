package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

type scopedLiveShares struct {
	interfaces.KBShareService
	grants map[uint64]map[string]types.OrgMemberRole
}

// Synthetic SQL grants model the authorization input only; they do not claim
// to implement production organization/KB-share SQL or PostgreSQL/schema3023.
type a23ScopeGrant struct {
	TenantID   uint64
	KBID       string
	Permission string
}

func (a23ScopeGrant) TableName() string { return "a23_scope_grants" }

type a23SQLShares struct {
	interfaces.KBShareService
	db      *gorm.DB
	checked func()
}

func (s *a23SQLShares) CheckTenantKBPermission(ctx context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	var row a23ScopeGrant
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND kb_id = ?", tenant, id).First(&row).Error
	if s.checked != nil {
		s.checked()
	}
	if err == gorm.ErrRecordNotFound {
		return "", false, nil
	}
	return types.OrgMemberRole(row.Permission), err == nil, err
}

type a23SQLCommitGate struct {
	interfaces.MCPEndpointRepository
	ready, resume chan struct{}
	trace         func(string)
}

func (r *a23SQLCommitGate) Update(ctx context.Context, ep *types.MCPEndpoint) error {
	if r.ready != nil {
		r.trace("before-commit")
		close(r.ready)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := r.MCPEndpointRepository.Update(ctx, ep)
	if r.trace != nil && err == nil {
		r.trace("commit")
	}
	return err
}

type a23RetrievalKB struct {
	*stubKBService
	queries atomic.Int32
}

func (s *a23RetrievalKB) GetKnowledgeBasesByIDsOnly(_ context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	var rows []*types.KnowledgeBase
	for _, id := range ids {
		if kb := s.kbs[id]; kb != nil {
			rows = append(rows, kb)
		}
	}
	return rows, nil
}

func (s *a23RetrievalKB) ResolveEmbeddingModelKeys(_ context.Context, _ []*types.KnowledgeBase) map[string]string {
	return map[string]string{}
}

func (s *a23RetrievalKB) HybridSearch(_ context.Context, _ string, _ types.SearchParams) ([]*types.SearchResult, error) {
	s.queries.Add(1)
	return []*types.SearchResult{}, nil
}

type a23HTTPHarness struct {
	db                         *gorm.DB
	engine                     *gin.Engine
	server                     *httptest.Server
	repo                       *a23SQLCommitGate
	shares                     *a23SQLShares
	kbs                        *a23RetrievalKB
	svc                        interfaces.MCPEndpointService
	admin, viewer, machine, id string
}

func newA23HTTPHarness(t *testing.T) *a23HTTPHarness {
	t.Helper()
	t.Setenv("JWT_SECRET", "a23-isolated-signing-key-for-tests-only")
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a23-http.sqlite")), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&types.User{}, &types.AuthToken{}, &types.Tenant{}, &types.TenantMember{}, &types.MCPEndpoint{}, &a23ScopeGrant{}); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []types.Tenant{{ID: 1, Name: "A23 tenant A", Status: "active"}, {ID: 2, Name: "A23 tenant B", Status: "active"}} {
		if err := db.Create(&tenant).Error; err != nil {
			t.Fatal(err)
		}
	}
	const password = "a23-isolated-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []types.User{{ID: "a23-admin", Username: "a23-admin", Email: "a23-admin@example.invalid", TenantID: 1, PasswordHash: string(hash), IsActive: true}, {ID: "a23-viewer", Username: "a23-viewer", Email: "a23-viewer@example.invalid", TenantID: 1, PasswordHash: string(hash), IsActive: true}} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []types.TenantMember{{UserID: "a23-admin", TenantID: 1, Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive}, {UserID: "a23-viewer", TenantID: 1, Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive}} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&a23ScopeGrant{TenantID: 1, KBID: "shared", Permission: "viewer"}).Error; err != nil {
		t.Fatal(err)
	}
	enforce := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforce}}
	userRepo, tokenRepo := repository.NewUserRepository(db), repository.NewAuthTokenRepository(db)
	tenants := service.NewTenantService(repository.NewTenantRepository(db), nil)
	members := service.NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, userRepo, tokenRepo)
	users := service.NewUserService(cfg, userRepo, tokenRepo, tenants, members, nil)
	h := &a23HTTPHarness{db: db, repo: &a23SQLCommitGate{MCPEndpointRepository: repository.NewMCPEndpointRepository(db)}, shares: &a23SQLShares{db: db}}
	for _, identity := range []struct {
		email string
		token *string
	}{{"a23-admin@example.invalid", &h.admin}, {"a23-viewer@example.invalid", &h.viewer}} {
		login, err := users.Login(context.Background(), &types.LoginRequest{Email: identity.email, Password: password})
		if err != nil || login == nil || !login.Success || login.Token == "" {
			t.Fatal("isolated real sign-in failed")
		}
		*identity.token = login.Token
	}
	h.kbs = &a23RetrievalKB{stubKBService: &stubKBService{kbs: map[string]*types.KnowledgeBase{
		"own":    {ID: "own", Name: "own", TenantID: 1, Type: "document", IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}},
		"shared": {ID: "shared", Name: "shared", TenantID: 2, Type: "document", IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}},
	}}}
	h.svc = service.NewMCPEndpointService(h.repo, h.kbs, h.shares, nil)
	h.engine = gin.New()
	h.engine.ContextWithFallback = true
	// Equivalent published management routes: actual Auth validates persisted
	// JWTs and live SQL membership, then the production RBAC guard and Handler.
	// No injected caller role, fake 403, or mock endpoint repository is used.
	mgmt := h.engine.Group("/api/v1/mcp-endpoints", middleware.Auth(tenants, users, members, nil, cfg), middleware.RequireRole(types.TenantRoleAdmin, cfg))
	endpointHandler := handler.NewMCPEndpointHandler(h.svc)
	mgmt.POST("", endpointHandler.CreateMCPEndpoint)
	mgmt.PUT("/:endpoint_id", endpointHandler.UpdateMCPEndpoint)
	mgmt.DELETE("/:endpoint_id", endpointHandler.DeleteMCPEndpoint)
	mgmt.POST("/:endpoint_id/rotate-token", endpointHandler.RotateMCPEndpointToken)
	// Auxiliary last_used is excluded from this scope-only harness; original
	// G/H lastTouch SQL/HTTP regressions remain unmodified in middleware.
	srv := NewServer(h.kbs, nil, nil, nil, nil, nil, nil, h.shares, tenants, nil, nil, cfg, nil, nil, nil, nil)
	h.engine.POST("/mcp/:endpoint_id", middleware.MCPEndpointAuth(h.svc, tenants), gin.WrapH(srv.Handler()))
	h.server = httptest.NewServer(h.engine)
	t.Cleanup(h.server.Close)
	code, body := h.request(t, h.admin, "POST", "/api/v1/mcp-endpoints", map[string]any{"name": "A23 machine", "enabled": true, "knowledge_base_ids": []string{"own", "shared"}, "tools": []string{"search_knowledge", "list_knowledge_bases"}})
	if code != 201 {
		t.Fatalf("admin create HTTP=%d", code)
	}
	var created struct{ Data struct{ ID, Token string } }
	if json.Unmarshal(body, &created) != nil || created.Data.ID == "" || created.Data.Token == "" {
		t.Fatal("invalid create reply")
	}
	h.id, h.machine = created.Data.ID, created.Data.Token
	return h
}

func (h *a23HTTPHarness) request(t *testing.T, token, method, path string, payload any, tenant ...string) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if len(tenant) > 0 {
		req.Header.Set("X-Tenant-ID", tenant[0])
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal("isolated HTTP request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) && token != "" && response.StatusCode >= 400 {
		t.Fatal("error response exposed credential")
	}
	return response.StatusCode, body
}

func TestA23MCPEndpointManagementHTTPPermissions(t *testing.T) {
	h := newA23HTTPHarness(t)
	paths := []struct{ method, path string }{{"POST", "/api/v1/mcp-endpoints"}, {"PUT", "/api/v1/mcp-endpoints/" + h.id}, {"POST", "/api/v1/mcp-endpoints/" + h.id + "/rotate-token"}, {"DELETE", "/api/v1/mcp-endpoints/" + h.id}}
	before, err := h.repo.GetByID(context.Background(), h.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		t.Run("viewer-"+p.method+"-"+p.path, func(t *testing.T) {
			code, _ := h.request(t, h.viewer, p.method, p.path, map[string]any{"name": "denied", "enabled": false})
			if code != 403 {
				t.Fatalf("viewer management HTTP=%d", code)
			}
			t.Logf("real-auth role=viewer method=%s HTTP=%d", p.method, code)
		})
		t.Run("machine-is-not-manager-"+p.method+"-"+p.path, func(t *testing.T) {
			code, _ := h.request(t, h.machine, p.method, p.path, map[string]any{"name": "denied"})
			if code != 401 {
				t.Fatalf("machine accepted on personal auth surface HTTP=%d", code)
			}
		})
	}
	after, err := h.repo.GetByID(context.Background(), h.id)
	if err != nil || after == nil || after.Name != before.Name || after.TokenHash != before.TokenHash || !after.Enabled {
		t.Fatal("denied request mutated endpoint")
	}
	var count int64
	if err := h.db.Model(&types.MCPEndpoint{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("viewer created or deleted SQL row")
	}
	foreign := &types.MCPEndpoint{TenantID: 2, Name: "foreign", Enabled: true, TokenHash: service.HashMCPEndpointToken("synthetic-foreign-only"), Tools: types.StringArray{"search_knowledge"}}
	if err := h.repo.Create(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths[1:] {
		code, _ := h.request(t, h.admin, p.method, strings.ReplaceAll(p.path, h.id, foreign.ID), map[string]any{"name": "denied"})
		if code != 404 {
			t.Fatalf("foreign endpoint management HTTP=%d", code)
		}
	}
	code, _ := h.request(t, h.admin, "POST", "/api/v1/mcp-endpoints", map[string]any{"name": "denied"}, "2")
	if code != 403 {
		t.Fatalf("foreign tenant-header HTTP=%d", code)
	}
	code, _ = h.request(t, "", "POST", "/api/v1/mcp-endpoints", map[string]any{"name": "denied"})
	if code != 401 {
		t.Fatalf("anonymous HTTP=%d", code)
	}
	code, _ = h.request(t, h.admin, "PUT", "/api/v1/mcp-endpoints/"+h.id, map[string]any{"name": "A23 updated"})
	if code != 200 {
		t.Fatal("admin update denied")
	}
	code, _ = h.request(t, h.admin, "POST", "/api/v1/mcp-endpoints/"+h.id+"/rotate-token", map[string]any{})
	if code != 200 {
		t.Fatal("admin rotation denied")
	}
	if _, err := h.svc.Authenticate(context.Background(), h.id, h.machine); err == nil {
		t.Fatal("old token accepted after admin rotation")
	}
	code, _ = h.request(t, h.admin, "DELETE", "/api/v1/mcp-endpoints/"+h.id, nil)
	if code != 200 {
		t.Fatalf("admin deletion HTTP=%d", code)
	}
}

func TestA23SharedRevocationAfterCheckBeforeCommitMCPHTTP(t *testing.T) {
	h := newA23HTTPHarness(t)
	probe := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "search_knowledge", "arguments": map[string]any{"query": "synthetic probe", "mode": "keyword", "knowledge_base_ids": []string{"shared"}}}}
	decode := func(body []byte) bool {
		raw := strings.TrimSpace(string(body))
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "data:") {
				raw = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				break
			}
		}
		var reply struct {
			Result *struct {
				IsError bool `json:"isError"`
			}
			Error json.RawMessage
		}
		if json.Unmarshal([]byte(raw), &reply) != nil || reply.Result == nil || len(reply.Error) != 0 {
			t.Fatal("MCP protocol error is not resource authorization evidence")
		}
		return reply.Result.IsError
	}
	initialCode, initialBody := h.request(t, h.machine, "POST", "/mcp/"+h.id, probe)
	if initialCode != 200 || decode(initialBody) || h.kbs.queries.Load() != 1 {
		t.Fatal("authorized control never reached real retrieval dispatch")
	}
	var mu sync.Mutex
	start := time.Now()
	var events []string
	trace := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		t.Logf("event=%s monotonic_ns=%d", event, time.Since(start).Nanoseconds())
	}
	h.repo.ready, h.repo.resume, h.repo.trace = make(chan struct{}), make(chan struct{}), trace
	h.shares.checked = func() { trace("authorization-complete") }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var once sync.Once
	release := func() { once.Do(func() { close(h.repo.resume) }) }
	t.Cleanup(release)
	done := make(chan int, 1)
	trace("save-start")
	go func() {
		code, _ := h.request(t, h.admin, "PUT", "/api/v1/mcp-endpoints/"+h.id, map[string]any{"knowledge_base_ids": []string{"own", "shared"}})
		done <- code
	}()
	select {
	case <-h.repo.ready:
	case <-ctx.Done():
		t.Fatal("save did not reach post-authorization barrier")
	}
	if err := h.db.Where("tenant_id = ? AND kb_id = ?", 1, "shared").Delete(&a23ScopeGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	trace("revocation-committed")
	release()
	var status int
	select {
	case status = <-done:
	case <-ctx.Done():
		t.Fatal("save did not finish")
	}
	if status != 200 {
		t.Fatalf("already-authorized config commit HTTP=%d", status)
	}
	stored, err := h.repo.GetByID(context.Background(), h.id)
	if err != nil || stored == nil || len(stored.KnowledgeBaseIDs) != 2 {
		t.Fatal("historical selection state lost")
	}
	code, body := h.request(t, h.machine, "POST", "/mcp/"+h.id, probe)
	if code != 200 || !decode(body) || h.kbs.queries.Load() != 1 {
		t.Fatal("post-revocation request reached retrieval")
	}
	trace("new-MCP-request-denied")
	t.Logf("MCP_HTTP=%d resource_denied=true retrieval_calls=%d saved_historical_kb_ids=%d", code, h.kbs.queries.Load(), len(stored.KnowledgeBaseIDs))
	mu.Lock()
	actual := strings.Join(events, ",")
	mu.Unlock()
	if !strings.HasPrefix(actual, "save-start,authorization-complete,before-commit,revocation-committed,commit") {
		t.Fatalf("unexpected linearization order: %s", fmt.Sprint(actual))
	}
}

func (s *scopedLiveShares) CheckTenantKBPermission(_ context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	role, ok := s.grants[tenant][id]
	return role, ok, nil
}

func TestMCPEndpointLiveSharedScopeRevalidation(t *testing.T) {
	own := &types.KnowledgeBase{ID: "own", TenantID: 1}
	shared := &types.KnowledgeBase{ID: "shared", TenantID: 2}
	foreign := &types.KnowledgeBase{ID: "foreign", TenantID: 3}
	srv := newScopeTestServer(own, shared, foreign)
	shares := &scopedLiveShares{grants: map[uint64]map[string]types.OrgMemberRole{1: {"shared": types.OrgRoleEditor}}}
	srv.kbShareService = shares
	ep := &types.MCPEndpoint{ID: "ep", TenantID: 1, KnowledgeBaseIDs: types.StringArray{"own", "shared", "foreign"}}
	ctx := mcpCallContext(1, ep)
	got, err := srv.allowedKnowledgeBases(ctx, ep)
	if err != nil || len(got) != 2 {
		t.Fatalf("own + live shared only: %v %v", knowledgeBaseIDs(got), err)
	}
	if _, err = srv.selectKnowledgeBases(ctx, ep, []string{"foreign"}); err == nil {
		t.Fatal("forged foreign ID accepted")
	}
	if _, err = srv.authorizedKnowledgeBase(mcpCallContext(4, ep), "shared", types.OrgRoleViewer); err == nil {
		t.Fatal("grant crossed tenant")
	}
	// Organization membership/share downgrade permits reads, but immediately revokes writes.
	shares.grants[1]["shared"] = types.OrgRoleViewer
	if _, err = srv.authorizedKnowledgeBase(ctx, "shared", types.OrgRoleEditor); err == nil {
		t.Fatal("downgraded share still permits write")
	}
	if _, err = srv.authorizedKnowledgeBase(ctx, "shared", types.OrgRoleViewer); err != nil {
		t.Fatal(err)
	}
	delete(shares.grants[1], "shared")
	if _, err = srv.selectKnowledgeBases(ctx, ep, []string{"shared"}); err == nil {
		t.Fatal("saved ID survived live revocation")
	}
	got, err = srv.allowedKnowledgeBases(ctx, ep)
	if err != nil || len(got) != 1 || got[0].ID != "own" {
		t.Fatalf("revoked share remains: %v %v", knowledgeBaseIDs(got), err)
	}
	if len(ep.KnowledgeBaseIDs) != 3 {
		t.Fatal("revalidation must not silently rewrite saved configuration")
	}
	// Existing all-workspace semantics stay owned-only; shared is opt-in by ID.
	shares.grants[1]["shared"] = types.OrgRoleViewer
	// Tightening this machine endpoint's explicit allowlist denies a still-shared
	// KB on the next request, independently of credential validity.
	ep.KnowledgeBaseIDs = types.StringArray{"own"}
	if _, err = srv.selectKnowledgeBases(mcpCallContext(1, ep), ep, []string{"shared"}); err == nil {
		t.Fatal("endpoint scope removal bypassed by an organization share")
	}
	all := &types.MCPEndpoint{ID: "all", TenantID: 1}
	got, err = srv.selectKnowledgeBases(mcpCallContext(1, all), all, nil)
	if err != nil || len(got) != 1 || got[0].ID != "own" {
		t.Fatalf("unrestricted scope changed: %v %v", knowledgeBaseIDs(got), err)
	}
}
