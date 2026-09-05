package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type userMCPTenantLookupStub struct {
	tenant *types.Tenant
	err    error
}

func (s userMCPTenantLookupStub) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, s.err
}

type userMCPUserLookupStub struct {
	user *types.User
	err  error
}

func (s userMCPUserLookupStub) GetUserByID(context.Context, string) (*types.User, error) {
	return s.user, s.err
}

type userMCPMembershipLookupStub struct {
	member *types.TenantMember
	err    error
}

func (s userMCPMembershipLookupStub) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return s.member, s.err
}

type userMCPScopeLookupStub struct {
	scope *types.APIKeyTenantScope
	err   error
}

func (s userMCPScopeLookupStub) GetUserMCPTenantScope(context.Context, uint64, uint64) (*types.APIKeyTenantScope, error) {
	return s.scope, s.err
}

func userMCPAuthTestContext(header string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	if header != "" {
		c.Request.Header.Set("X-Tenant-ID", header)
	}
	return c, w
}

func userMCPAuthTestDependencies() (tenantLookup, userLookup, membershipLookup, userMCPKeyScopeLookup) {
	return userMCPTenantLookupStub{tenant: &types.Tenant{ID: 42, Status: "active"}},
		userMCPUserLookupStub{user: &types.User{ID: "user-1", IsActive: true}},
		userMCPMembershipLookupStub{member: &types.TenantMember{TenantID: 42, UserID: "user-1", Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive}},
		userMCPScopeLookupStub{scope: &types.APIKeyTenantScope{TenantID: 42, KBScopeMode: types.APIKeyKBScopeSelected, KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{KnowledgeBaseID: "kb-1"}}}}
}

func userMCPAuthTestKey() *types.TenantAPIKey {
	owner := "user-1"
	return &types.TenantAPIKey{ID: 9, OwnerUserID: &owner, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve", "chat", "read_agents"}}
}

func TestAuthenticateUserMCPKeyRequiresValidWorkspaceHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantSvc, userSvc, memberSvc, scopeSvc := userMCPAuthTestDependencies()

	c, w := userMCPAuthTestContext("")
	if authenticateUserMCPKeyRequest(c, tenantSvc, userSvc, memberSvc, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("missing workspace header authenticated")
	}
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"TENANT_REQUIRED"`) {
		t.Fatalf("missing header response = %d %s", w.Code, w.Body.String())
	}

	c, w = userMCPAuthTestContext("not-a-number")
	if authenticateUserMCPKeyRequest(c, tenantSvc, userSvc, memberSvc, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("invalid workspace header authenticated")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid header status = %d, want 400", w.Code)
	}
}

func TestAuthenticateUserMCPKeyRejectsUnauthorizedWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantSvc, userSvc, memberSvc, _ := userMCPAuthTestDependencies()
	c, w := userMCPAuthTestContext("42")
	scopeSvc := userMCPScopeLookupStub{err: errors.New("not found")}

	if authenticateUserMCPKeyRequest(c, tenantSvc, userSvc, memberSvc, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("unauthorized workspace authenticated")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthorized workspace status = %d, want 403", w.Code)
	}
}

func TestAuthenticateUserMCPKeyRechecksOwnerAndMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantSvc, _, _, scopeSvc := userMCPAuthTestDependencies()

	c, w := userMCPAuthTestContext("42")
	inactiveUser := userMCPUserLookupStub{user: &types.User{ID: "user-1", IsActive: false}}
	activeMember := userMCPMembershipLookupStub{member: &types.TenantMember{Status: types.TenantMemberStatusActive}}
	if authenticateUserMCPKeyRequest(c, tenantSvc, inactiveUser, activeMember, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("inactive owner authenticated")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("inactive owner status = %d, want 403", w.Code)
	}

	c, w = userMCPAuthTestContext("42")
	activeUser := userMCPUserLookupStub{user: &types.User{ID: "user-1", IsActive: true}}
	inactiveMember := userMCPMembershipLookupStub{member: &types.TenantMember{Status: types.TenantMemberStatusSuspended}}
	if authenticateUserMCPKeyRequest(c, tenantSvc, activeUser, inactiveMember, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("inactive membership authenticated")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("inactive membership status = %d, want 403", w.Code)
	}
}

func TestAuthenticateUserMCPKeyInjectsCurrentRoleAndRestrictedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantSvc, userSvc, memberSvc, scopeSvc := userMCPAuthTestDependencies()
	c, w := userMCPAuthTestContext("42")

	if !authenticateUserMCPKeyRequest(c, tenantSvc, userSvc, memberSvc, scopeSvc, userMCPAuthTestKey()) {
		t.Fatalf("valid key rejected: %d %s", w.Code, w.Body.String())
	}
	ctx := c.Request.Context()
	if tenantID, ok := types.TenantIDFromContext(ctx); !ok || tenantID != 42 {
		t.Fatalf("tenant context = %d, ok=%v", tenantID, ok)
	}
	if role := types.TenantRoleFromContext(ctx); role != types.TenantRoleViewer {
		t.Fatalf("role context = %q, want viewer", role)
	}
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || !scope.IsKnowledgeBaseRestricted() || !scope.AllowsKnowledgeBase("kb-1") || scope.AllowsKnowledgeBase("kb-2") {
		t.Fatalf("API key scope = %#v, ok=%v", scope, ok)
	}
}

func TestAuthenticateUserMCPKeyRejectsInactiveWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, userSvc, memberSvc, scopeSvc := userMCPAuthTestDependencies()
	tenantSvc := userMCPTenantLookupStub{tenant: &types.Tenant{ID: 42, Status: "suspended"}}
	c, w := userMCPAuthTestContext("42")

	if authenticateUserMCPKeyRequest(c, tenantSvc, userSvc, memberSvc, scopeSvc, userMCPAuthTestKey()) {
		t.Fatal("inactive workspace authenticated")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("inactive workspace status = %d, want 400", w.Code)
	}
}
