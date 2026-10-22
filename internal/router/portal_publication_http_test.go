package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	"gorm.io/gorm"
)

type portalM7Members struct{ documentTestMembers }

func TestPortalM7PersonalKeyDefaultExpiryHTTP(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}}
	g := gin.New()
	tokens := portalM7Management(t, f.DB, g, cfg)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+tokens["A"])
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	create := func(expiry *int64) types.TenantAPIKey {
		body := map[string]any{"name": "synthetic-unified-personal", "client_type": "generic", "capabilities": []string{"retrieve", "enterprise_retrieve"}, "tenant_scopes": []any{map[string]any{"tenant_id": 1, "kb_scope_mode": "all", "knowledge_bases": []any{}}}}
		if expiry != nil {
			body["expires_at_unix"] = *expiry
		}
		w := request("POST", "/api/v1/mcp-api-keys", body)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var response struct {
			Data struct {
				ID    uint64 `json:"id"`
				Token string `json:"token"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data.Token)
		var key types.TenantAPIKey
		require.NoError(t, f.DB.Where("id=?", response.Data.ID).Take(&key).Error)
		return key
	}
	priorExpiry := time.Now().UTC().Add(12 * time.Hour).Truncate(time.Second)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET expires_at=? WHERE id=3", priorExpiry).Error)
	key := create(nil)
	require.Nil(t, key.ExpiresAt, "omitted expiry creates an unlimited personal Key")
	explicit := time.Now().UTC().Add(90 * 24 * time.Hour).Unix()
	dated := create(&explicit)
	require.NotNil(t, dated.ExpiresAt)
	require.Equal(t, explicit, dated.ExpiresAt.Unix())
	path := "/api/v1/mcp-api-keys/" + fmt.Sprint(dated.ID)
	w := request("PATCH", path, map[string]any{"name": "renamed-without-expiry-change"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request("POST", path+"/rotate", map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, f.DB.Where("id=?", dated.ID).Take(&dated).Error)
	require.Equal(t, explicit, dated.ExpiresAt.Unix(), "PATCH and rotate preserve an existing chosen expiry")
	var old types.TenantAPIKey
	require.NoError(t, f.DB.Where("id=3").Take(&old).Error)
	require.Equal(t, priorExpiry.Unix(), old.ExpiresAt.Unix())
	require.NotContains(t, []string(old.Capabilities), "enterprise_retrieve", "new default does not expand old Keys")
}

func (m portalM7Members) ListByUser(ctx context.Context, user string) ([]*types.TenantMember, error) {
	return m.repo.ListByUser(ctx, user)
}

type portalM7KBs struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (s portalM7KBs) ListKnowledgeBasesByTenantID(ctx context.Context, tenant uint64) ([]*types.KnowledgeBase, error) {
	return s.repo.ListKnowledgeBasesByTenantID(ctx, tenant)
}
func (s portalM7KBs) GetKnowledgeBaseByIDOnly(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return s.repo.GetKnowledgeBaseByID(ctx, id)
}

// Real token issuance, existing JWT auth and route guards; only user records
// and policy enablement are synthetic. No SID login or HR check is simulated.
func portalM7Management(t *testing.T, db *gorm.DB, g *gin.Engine, cfg *config.Config) map[string]string {
	t.Helper()
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	require.NoError(t, db.Exec("UPDATE users SET is_system_admin=true WHERE id='reviewer'").Error)
	members := portalM7Members{documentTestMembers{repo: repository.NewTenantMemberRepository(db)}}
	tenants := documentTestTenants{repo: repository.NewTenantRepository(db)}
	users := service.NewUserService(cfg, repository.NewUserRepository(db), repository.NewAuthTokenRepository(db), tenants, members, nil)
	portal := service.NewConfiguredPortalService(cfg, db, repository.NewPortalRepository(db), tenants, members, nil, nil)
	if cfg.PersonalMCP == nil {
		require.NoError(t, service.ConfigurePortalPublication(portal, service.PortalPolicyAuthorityConfig{Enabled: true, AllowSystemAdminReview: true, ContractRef: "ISOLATED_OWNER_ADMIN_USER_ACCEPTED_MAPPING"}))
	}
	guards := &rbacGuards{cfg: cfg}
	r := g.Group("/api/v1")
	r.Use(middleware.Auth(tenants, users, members, nil, cfg), guards.ensureAPIKeyAuthorizer().Middleware(), middleware.ErrorHandler())
	RegisterPortalRoutes(r, handler.NewPortalHandler(portal), guards)
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(db))
	RegisterUserMCPAPIKeyRoutes(r, handler.NewTenantHandler(tenants, keys, users, members, portalM7KBs{repo: repository.NewKnowledgeBaseRepository(db)}, nil, nil, cfg, nil))
	tokens := map[string]string{}
	for _, id := range []string{"owner", "reviewer", "A"} {
		u, err := repository.NewUserRepository(db).GetUserByID(context.Background(), id)
		require.NoError(t, err)
		token, _, err := users.GenerateTokens(context.Background(), u)
		require.NoError(t, err)
		tokens[id] = token
	}
	return tokens
}

func TestPortalM7JWTHTTPFirstConfirmation(t *testing.T) {
	f := fixture.New(t)
	f.Portal(t)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}}
	g := gin.New()
	tokens := portalM7Management(t, f.DB, g, cfg)
	request := func(token, method, path string, body any) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if token == tokens["owner"] {
			r.Header.Set("X-Tenant-ID", "99")
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	owner := "/api/v1/tenants/99/knowledge-publication/public-kb"
	admin := "/api/v1/system/admin/portal/spaces/99/knowledge-publication/public-kb"
	snapshot := func(token, path string) []service.PortalReviewResource {
		w := request(token, "GET", path, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Data service.PortalPublicationReviewSnapshot
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result.Data.Resources
	}
	body := map[string]any{"reason": "synthetic full HTTP owner review", "inherit_new_ordinary": true, "resources": snapshot(tokens["owner"], owner)}
	w := request(tokens["A"], "POST", owner+"/propose", body)
	require.Equal(t, 403, w.Code, w.Body.String())
	w = request(tokens["owner"], "POST", owner+"/propose", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	body["resources"] = snapshot(tokens["reviewer"], admin)
	w = request(tokens["owner"], "POST", owner+"/approve", body)
	require.Equal(t, 403, w.Code, w.Body.String())
	w = request(tokens["reviewer"], "POST", admin+"/approve", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(tokens["owner"], "GET", owner+"/history", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "enterprise.policy_approve")
	body["resources"] = []service.PortalReviewResource{{Kind: "kb", ID: "public-kb", Revision: 0}}
	w = request(tokens["owner"], "POST", owner+"/revoke", body)
	require.Equal(t, 200, w.Code, w.Body.String())
}
