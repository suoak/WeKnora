package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Real migrated synthetic DB, issued JWTs, Auth/RBAC, DocumentReader grants,
// services/repositories and TCP HTTP. No caller-controlled role headers and no
// production connection. Restricted totals are omitted, never substituted with
// global totals or a fake visible_count=0.
func TestPortalAssetCountsAuthenticatedHTTPPermissionAcceptance(t *testing.T) {
	f := fixture.New(t)
	f.Portal(t)
	require.NoError(t, f.DB.Exec("UPDATE users SET is_system_admin=true WHERE id='reviewer'").Error)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	require.NoError(t, f.DB.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES('manual-asset',99,'unknown-kb','manual','synthetic manual','synthetic-local','manual.md','failed','enabled','web')").Error)
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
	tenants := documentTestTenants{repo: repository.NewTenantRepository(f.DB)}
	members := portalM7Members{documentTestMembers{repo: repository.NewTenantMemberRepository(f.DB)}}
	users := service.NewUserService(cfg, repository.NewUserRepository(f.DB), repository.NewAuthTokenRepository(f.DB), tenants, members, nil)
	portal := service.NewConfiguredPortalService(cfg, f.DB, repository.NewPortalRepository(f.DB), tenants, members, nil, nil)
	kh := handler.NewKnowledgeHandler(cfg, ks, nil, nil, nil, cs, nil, nil, nil, nil, nil)
	ch := handler.NewChunkHandler(cs, ks)
	guards := newRBACGuards(cfg, nil, nil, kh, ch, nil, nil, ks, cs, nil, nil)
	boundary := middleware.NewDocumentCoreReadBoundary(ks, cs)
	e := gin.New()
	e.Use(middleware.ErrorHandler(), boundary.Outer())
	e.Use(middleware.Auth(tenants, users, members, nil, cfg), boundary.Inner(), guards.apiKeyAuthorizer.Middleware())
	r := e.Group("/api/v1")
	RegisterPortalRoutes(r, handler.NewPortalHandler(portal), guards)
	RegisterKnowledgeRoutes(r, kh, guards)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	tokens := map[string]string{}
	for _, id := range []string{"reviewer", "owner", "A", "B", "D"} {
		u, e := repository.NewUserRepository(f.DB).GetUserByID(context.Background(), id)
		require.NoError(t, e)
		token, _, e := users.GenerateTokens(context.Background(), u)
		require.NoError(t, e)
		tokens[id] = token
	}
	get := func(user, tenant, path string) (int, []byte) {
		t.Helper()
		req, e := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+tokens[user])
		req.Header.Set("X-Tenant-ID", tenant)
		resp, e := server.Client().Do(req)
		require.NoError(t, e)
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		require.NoError(t, e)
		return resp.StatusCode, raw
	}
	for _, tc := range []struct {
		user, tenant string
		totals       bool
	}{
		{"reviewer", "1", true}, {"owner", "99", true},
		{"B", "99", false}, {"D", "99", false}, {"A", "1", false},
	} {
		t.Run(tc.user, func(t *testing.T) {
			code, raw := get(tc.user, tc.tenant, "/api/v1/portal/spaces")
			require.Equal(t, http.StatusOK, code, string(raw))
			var response struct {
				Data []types.PortalSpaceResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(raw, &response))
			var row *types.PortalSpaceResponse
			for _, item := range response.Data {
				if item.TenantID == 99 {
					copy := item
					row = &copy
				}
			}
			require.NotNil(t, row)
			if tc.totals {
				require.Equal(t, "space_total", row.AssetCountScope)
				require.EqualValues(t, 7, *row.DocumentEntryCount)
				require.EqualValues(t, 6, *row.UploadedFileCount)
			} else {
				require.Equal(t, "hidden", row.AssetCountScope)
				require.Nil(t, row.DocumentEntryCount)
				require.Nil(t, row.UploadedFileCount)
			}
			require.NotContains(t, string(raw), fixture.Canary)
			require.NotContains(t, string(raw), "manual-asset")
		})
	}
	for _, tc := range []struct {
		user, path string
		status     int
	}{
		{"B", "/api/v1/knowledge/public-doc", 200},
		{"B", "/api/v1/knowledge/sensitive", 403},
		{"D", "/api/v1/knowledge/sensitive", 200},
		{"D", "/api/v1/knowledge/other-sensitive", 403},
	} {
		code, raw := get(tc.user, "99", tc.path)
		require.Equal(t, tc.status, code, string(raw))
		if code != 200 {
			require.NotContains(t, string(raw), fixture.Canary)
		}
	}
	code, raw := get("D", "99", "/api/v1/knowledge-bases/private-kb/knowledge?page=1&page_size=100")
	require.Equal(t, http.StatusOK, code, string(raw))
	var visible struct {
		Total int64             `json:"total"`
		Data  []types.Knowledge `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &visible))
	require.EqualValues(t, 2, visible.Total, "visible total uses the actual grants, independently of workspace asset totals")
	for _, row := range visible.Data {
		require.NotEqual(t, "other-sensitive", row.ID)
	}
}
