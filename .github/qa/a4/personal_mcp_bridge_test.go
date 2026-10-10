package middleware_test

// Test-only loopback fixture. It intentionally does not run application migrations
// or AutoMigrate. SQL below builds disposable test tables, not schema3023.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	repo "github.com/Tencent/WeKnora/internal/application/repository"
	svc "github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	mw "github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gl "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type personalFixture struct {
	t         *testing.T
	db        *gorm.DB
	server    *httptest.Server
	keys      interfaces.TenantAPIKeyService
	userKeys  interfaces.UserMCPAPIKeyService
	members   interfaces.TenantMemberService
	tokens    map[string]string
	ids       map[string]uint64
	retrieved atomic.Int64
	stopped   chan struct{}
}

// This schema-derived DDL is deliberately test-only, without implicit defaults,
// production migrations or claimed PostgreSQL constraint parity.
func personalTables(t *testing.T, db *gorm.DB) {
	models := []any{&types.User{}, &types.Tenant{}, &types.TenantMember{}, &types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}, &types.KnowledgeBase{}, &types.KnowledgeBaseShare{}, &types.Organization{}, &types.OrganizationTenantMember{}}
	for _, model := range models {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		require.NoError(t, err)
		cols := []string{}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			typ := "TEXT"
			switch f.GORMDataType {
			case schema.Int, schema.Uint:
				typ = "INTEGER"
			case schema.Bool:
				typ = "BOOLEAN"
			case schema.Time:
				typ = "DATETIME"
			case schema.Float:
				typ = "REAL"
			case schema.Bytes:
				typ = "BLOB"
			}
			if f.PrimaryKey {
				typ += " PRIMARY KEY"
			}
			cols = append(cols, fmt.Sprintf("\"%s\" %s", f.DBName, typ))
		}
		if s.Table == "api_key_tenant_scopes" {
			cols = append(cols, "FOREIGN KEY(api_key_id) REFERENCES tenant_api_keys(id) ON DELETE CASCADE")
		}
		if s.Table == "api_key_kb_scopes" {
			cols = append(cols, "FOREIGN KEY(api_key_tenant_scope_id) REFERENCES api_key_tenant_scopes(id) ON DELETE CASCADE")
		}
		require.NoError(t, db.Exec(fmt.Sprintf("CREATE TABLE \"%s\" (%s)", s.Table, strings.Join(cols, ","))).Error)
	}
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX personal_member_live ON tenant_members(user_id,tenant_id) WHERE deleted_at IS NULL").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX personal_key_hash ON tenant_api_keys(key_hash)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX personal_key_tenant ON api_key_tenant_scopes(api_key_id,tenant_id)").Error)
}

func newPersonalFixture(t *testing.T) *personalFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_SECRET", "synthetic-personal-mcp-tests-not-production")
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_foreign_keys=1"), &gorm.Config{Logger: gl.Default.LogMode(gl.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	personalTables(t, db)
	for _, name := range []string{"Alice", "Bob"} {
		require.NoError(t, db.Table("users").Create(map[string]any{"id": name, "username": name, "email": name + "@synthetic.invalid", "is_active": true, "tenant_id": 1}).Error)
	}
	for _, id := range []uint64{1, 2, 3} {
		require.NoError(t, db.Table("tenants").Create(map[string]any{"id": id, "name": "synthetic", "status": "active"}).Error)
	}
	ms := svc.NewTenantMemberService(repo.NewTenantMemberRepository(db), nil, nil, nil)
	for _, u := range []string{"Alice", "Bob"} {
		for _, tenant := range []uint64{1, 2} {
			_, err = ms.AddMember(context.Background(), u, tenant, types.TenantRoleViewer, nil)
			require.NoError(t, err)
		}
	}
	for _, kb := range []struct {
		id     string
		tenant uint64
	}{{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 1}, {"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 2}, {"cccccccc-cccc-4ccc-8ccc-cccccccccccc", 3}} {
		require.NoError(t, db.Table("knowledge_bases").Create(map[string]any{"id": kb.id, "tenant_id": kb.tenant, "name": "synthetic"}).Error)
	}
	require.NoError(t, db.Table("organizations").Create(map[string]any{"id": "org-synthetic", "name": "synthetic"}).Error)
	for _, tenant := range []uint64{1, 3} {
		require.NoError(t, db.Table("organization_tenant_members").Create(map[string]any{"id": fmt.Sprint(tenant), "organization_id": "org-synthetic", "tenant_id": tenant, "role": "viewer"}).Error)
	}
	require.NoError(t, db.Table("kb_shares").Create(map[string]any{"id": "share-original", "knowledge_base_id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc", "organization_id": "org-synthetic", "source_tenant_id": 3, "permission": "viewer"}).Error)
	ks := svc.NewTenantAPIKeyService(repo.NewTenantAPIKeyRepository(db))
	f := &personalFixture{t: t, db: db, keys: ks, userKeys: ks.(interfaces.UserMCPAPIKeyService), members: ms, tokens: map[string]string{}, ids: map[string]uint64{}, stopped: make(chan struct{})}
	for _, c := range []struct{ alias, owner string }{{"alice-1", "Alice"}, {"alice-2", "Alice"}, {"bob-1", "Bob"}} {
		res, e := ks.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: c.owner, Name: c.alias, Capabilities: []string{"retrieve"}, TenantScopes: []types.APIKeyTenantScope{{TenantID: 1, KBScopeMode: types.APIKeyKBScopeAll}, {TenantID: 2, KBScopeMode: types.APIKeyKBScopeAll}}})
		require.NoError(t, e)
		f.tokens[c.alias] = res.Token
		f.ids[c.alias] = res.APIKey.ID
	}
	// Non-personal compatibility characterization is confined to the isolated bridge.
	if os.Getenv("PERSONAL_MCP_TEST_BRIDGE") == "1" {
		for _, scope := range []types.APIKeyScopeType{types.APIKeyScopeTenant, types.APIKeyScopePlatform} {
			res, err := ks.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{
				ScopeType: scope, TenantID: 1, Name: "synthetic-non-personal", Capabilities: []string{"retrieve"},
			})
			require.NoError(t, err)
			f.tokens[string(scope)+"-key"] = res.Token
		}
	}
	ts := svc.NewTenantService(repo.NewTenantRepository(db), nil)
	us := svc.NewUserService(&config.Config{}, repo.NewUserRepository(db), nil, ts, ms, nil)
	shares := svc.NewKBShareService(repo.NewKBShareRepository(db), repo.NewOrganizationRepository(db), nil, nil, nil, nil)
	r := gin.New()
	r.Use(mw.ErrorHandler())
	// Explicit test control routes exist only on this loopback httptest listener.
	r.POST("/__test/control", func(c *gin.Context) {
		var op struct {
			Action string `json:"action"`
		}
		if c.ShouldBindJSON(&op) != nil {
			c.AbortWithStatus(400)
			return
		}
		var e error
		switch op.Action {
		case "disable":
			e = db.Table("users").Where("id=?", "Alice").Update("is_active", false).Error
		case "restore":
			e = db.Table("users").Where("id=?", "Alice").Update("is_active", true).Error
		case "remove":
			e = ms.RemoveMember(c, "Alice", 1)
		case "rejoin":
			_, e = ms.AddMember(c, "Alice", 1, types.TenantRoleViewer, nil)
		case "revoke":
			e = f.userKeys.RevokeUserMCPAPIKey(c, "Alice", f.ids["alice-1"])
		case "rotate":
			var rotated *interfaces.TenantAPIKeyCreateResult
			rotated, e = f.userKeys.RotateUserMCPAPIKey(c, "Alice", f.ids["alice-1"], interfaces.UserMCPAPIKeyRotateRequest{})
			if e == nil {
				f.tokens["rotated"] = rotated.Token
			}
		case "stop":
			close(f.stopped)
		default:
			c.AbortWithStatus(400)
			return
		}
		if e != nil {
			c.AbortWithStatus(500)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/__test/credentials", func(c *gin.Context) { c.JSON(200, f.tokens) }) // private synthetic transport; never log response
	r.GET("/__test/count", func(c *gin.Context) { c.JSON(200, gin.H{"retrieved": f.retrieved.Load()}) })
	a := mw.NewAPIKeyRouteAuthorizer()
	a.Register("GET", "/api/v1/auth/me", mw.APIKeyRoutePolicy{})
	a.Register("POST", "/api/v1/knowledge-bases/:id/hybrid-search", mw.APIKeyRoutePolicy{Capabilities: []types.APIKeyCapability{types.APIKeyCapabilityRetrieve}})
	v := r.Group("/api/v1", mw.Auth(ts, us, ms, ks, &config.Config{}), a.Middleware())
	v.GET("/auth/me", func(c *gin.Context) { c.JSON(200, gin.H{"owner": c.GetString(types.UserIDContextKey.String())}) })
	v.POST("/knowledge-bases/:id/hybrid-search", func(c *gin.Context) {
		var kb types.KnowledgeBase
		if db.WithContext(c).First(&kb, "id=?", c.Param("id")).Error != nil {
			c.AbortWithStatus(404)
			return
		}
		_, e := access.ResolveKB(c.Request.Context(), access.KBRequest{Caller: types.CallerFromContext(c.Request.Context())}, &kb, types.OrgRoleViewer, shares, nil)
		if e != nil {
			c.AbortWithStatusJSON(403, gin.H{"error": "resource forbidden"})
			return
		}
		f.retrieved.Add(1)
		c.JSON(200, gin.H{"data": []string{"synthetic result"}, "owner": c.GetString(types.UserIDContextKey.String())})
	})
	h := handler.NewTenantHandler(ts, ks, us, ms, nil, shares, nil, &config.Config{}, nil)
	// Actual Handler reached only with synthetic web identity injected on this test route.
	// This route is not evidence for JWT authentication; resource routes above use Auth.
	r.POST("/__test/rotate-handler", func(c *gin.Context) {
		c.Set(types.UserIDContextKey.String(), "Alice")
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(f.ids["alice-1"])}}
		h.RotateUserMCPAPIKey(c)
	})
	f.server = httptest.NewServer(r)
	t.Cleanup(func() { f.server.Close() })
	return f
}

func (f *personalFixture) request(alias string, tenant uint64, kb string) (int, string) {
	f.t.Helper()
	req, err := http.NewRequest("POST", f.server.URL+"/api/v1/knowledge-bases/"+kb+"/hybrid-search", strings.NewReader(`{"query_text":"synthetic"}`))
	require.NoError(f.t, err)
	req.Header.Set("X-API-Key", f.tokens[alias])
	if tenant != 0 {
		req.Header.Set("X-Tenant-ID", fmt.Sprint(tenant))
	}
	response, err := f.server.Client().Do(req)
	require.NoError(f.t, err)
	defer response.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(response.Body).Decode(&data)
	owner, _ := data["owner"].(string)
	return response.StatusCode, owner
}

func TestPersonalMCPLifecycleHTTP(t *testing.T) {
	ctx := context.Background()
	kb := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	t.Run("L01_owner_and_clients", func(t *testing.T) {
		f := newPersonalFixture(t)
		for _, row := range []struct{ alias, owner string }{{"alice-1", "Alice"}, {"alice-2", "Alice"}, {"bob-1", "Bob"}} {
			status, owner := f.request(row.alias, 1, kb)
			require.Equal(t, 200, status)
			require.Equal(t, row.owner, owner)
		}
		require.Equal(t, int64(3), f.retrieved.Load())
	})
	t.Run("L02_L03_disable_restore_characterization", func(t *testing.T) {
		f := newPersonalFixture(t)
		require.NoError(t, f.db.Table("users").Where("id=?", "Alice").Update("is_active", false).Error)
		status, _ := f.request("alice-1", 1, kb)
		require.Equal(t, 403, status)
		require.Zero(t, f.retrieved.Load())
		require.NoError(t, f.db.Table("users").Where("id=?", "Alice").Update("is_active", true).Error)
		status, _ = f.request("alice-1", 1, kb)
		require.Equal(t, 200, status, "characterization: old unrevoked key becomes usable again")
		row, e := f.userKeys.GetUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"])
		require.NoError(t, e)
		require.Nil(t, row.RevokedAt)
		require.Len(t, row.TenantScopes, 2)
		require.Equal(t, int64(1), f.retrieved.Load())
		t.Log("L03 restored active=true member=active revoked=false scopes=2 HTTP=200 retrieval=1; not a security-remediation PASS")
	})
	t.Run("L04_L05_remove_rejoin_characterization", func(t *testing.T) {
		f := newPersonalFixture(t)
		require.NoError(t, f.members.RemoveMember(ctx, "Alice", 1))
		status, _ := f.request("alice-1", 1, kb)
		require.Equal(t, 403, status)
		status, _ = f.request("alice-1", 2, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
		require.Equal(t, 200, status)
		_, e := f.members.AddMember(ctx, "Alice", 1, types.TenantRoleViewer, nil)
		require.NoError(t, e)
		status, _ = f.request("alice-1", 1, kb)
		require.Equal(t, 200, status)
		row, e := f.userKeys.GetUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"])
		require.NoError(t, e)
		require.Nil(t, row.RevokedAt)
		require.Len(t, row.TenantScopes, 2)
		t.Log("L05 rejoin member=active revoked=false scopes=2 HTTP=200; characterization only")
	})
	t.Run("L08_L09_rotate_revoke", func(t *testing.T) {
		f := newPersonalFixture(t)
		res, e := f.userKeys.RotateUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"], interfaces.UserMCPAPIKeyRotateRequest{})
		require.NoError(t, e)
		f.tokens["new"] = res.Token
		status, _ := f.request("alice-1", 1, kb)
		require.Equal(t, 401, status)
		status, _ = f.request("new", 1, kb)
		require.Equal(t, 200, status)
		status, _ = f.request("alice-2", 1, kb)
		require.Equal(t, 200, status)
		require.NoError(t, f.userKeys.RevokeUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"]))
		status, _ = f.request("new", 1, kb)
		require.Equal(t, 401, status)
		status, _ = f.request("bob-1", 1, kb)
		require.Equal(t, 200, status)
	})
	t.Run("L06_L07_selected_and_all_reshare", func(t *testing.T) {
		f := newPersonalFixture(t)
		shared := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		shareID := "share-original"
		row, e := f.userKeys.GetUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"])
		require.NoError(t, e)
		row.TenantScopes = []types.APIKeyTenantScope{{TenantID: 1, KBScopeMode: types.APIKeyKBScopeSelected, KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{KnowledgeBaseID: shared, SourceType: types.APIKeyKBSourceShared, KBShareID: &shareID}}}}
		_, e = f.userKeys.ReplaceUserMCPAPIKey(ctx, "Alice", row)
		require.NoError(t, e)
		status, _ := f.request("alice-1", 1, shared)
		require.Equal(t, 200, status)
		require.NoError(t, f.db.Where("id=?", shareID).Delete(&types.KnowledgeBaseShare{}).Error)
		before := f.retrieved.Load()
		status, _ = f.request("alice-1", 1, shared)
		require.Equal(t, 403, status)
		status, _ = f.request("alice-2", 1, shared)
		require.Equal(t, 403, status)
		require.Equal(t, before, f.retrieved.Load())
		require.NoError(t, f.db.Table("kb_shares").Create(map[string]any{"id": "share-recreated", "knowledge_base_id": shared, "organization_id": "org-synthetic", "source_tenant_id": 3, "permission": "viewer"}).Error)
		status, _ = f.request("alice-1", 1, shared)
		require.Equal(t, 403, status, "old selected share must not restore")
		status, _ = f.request("alice-2", 1, shared)
		require.Equal(t, 200, status, "all dynamically follows a new valid resource grant")
	})
	t.Run("L10_headers_scope_management", func(t *testing.T) {
		f := newPersonalFixture(t)
		status, _ := f.request("alice-1", 0, kb)
		require.Equal(t, 409, status)
		status, _ = f.request("alice-1", 3, kb)
		require.Equal(t, 403, status)
		status, _ = f.request("alice-1", 1, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
		require.Equal(t, 403, status)
		require.Zero(t, f.retrieved.Load())
	})
	t.Run("L11_inflight_revoke_barrier", func(t *testing.T) {
		f := newPersonalFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, e := f.keys.AuthenticateAPIKey(ctx, f.tokens["alice-1"])
			close(entered)
			<-release
			done <- e
		}()
		<-entered
		require.NoError(t, f.userKeys.RevokeUserMCPAPIKey(ctx, "Alice", f.ids["alice-1"]))
		status, _ := f.request("alice-1", 1, kb)
		require.Equal(t, 401, status)
		close(release)
		require.NoError(t, <-done)
		t.Log("Pre-revoke authenticated caller retained earlier result; post-commit fresh HTTP rejected")
	})
	t.Run("L12_tenant_key_passthrough_upstream", func(t *testing.T) {
		f := newPersonalFixture(t)
		res, e := f.keys.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeTenant, TenantID: 1, Name: "synthetic-tenant", Capabilities: []string{"retrieve"}})
		require.NoError(t, e)
		f.tokens["tenant-key"] = res.Token
		status, _ := f.request("tenant-key", 1, kb)
		require.Equal(t, 200, status, "APP accepts scoped tenant key; Python acceptance must be measured separately")
	})
}

func TestPersonalMCPHandlerErrorCanary(t *testing.T) {
	f := newPersonalFixture(t)
	const canary = "SYNTHETIC_SQL_CREDENTIAL_CANARY_NOT_REAL"
	require.NoError(t, f.db.Exec("CREATE TRIGGER personal_injected_error BEFORE UPDATE ON tenant_api_keys BEGIN SELECT RAISE(ABORT, '"+canary+"'); END").Error)
	response, e := f.server.Client().Post(f.server.URL+"/__test/rotate-handler", "application/json", strings.NewReader(`{}`))
	require.NoError(t, e)
	defer response.Body.Close()
	var body any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	data, e := json.Marshal(body)
	require.NoError(t, e)
	// Never include the body in failure output. A failure here is a real gap, not
	// authorization to modify the production error handling.
	if strings.Contains(string(data), canary) {
		t.Fatal("Actual Handler API error disclosed synthetic SQL canary (body intentionally withheld)")
	}
	require.GreaterOrEqual(t, response.StatusCode, 400)
}

func TestPersonalMCPLoopbackBridge(t *testing.T) {
	if os.Getenv("PERSONAL_MCP_TEST_BRIDGE") != "1" {
		t.Skip("loopback bridge requires explicit Python harness invocation")
	}
	f := newPersonalFixture(t)
	fmt.Println("PERSONAL_MCP_LOOPBACK=" + f.server.URL)
	select {
	case <-f.stopped:
	case <-time.After(3 * time.Minute):
		t.Fatal("loopback bridge was not stopped within bounded test window")
	}
}
