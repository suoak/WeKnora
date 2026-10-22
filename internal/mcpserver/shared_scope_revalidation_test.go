package mcpserver

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type scopedLiveShares struct {
	interfaces.KBShareService
	grants map[uint64]map[string]types.OrgMemberRole
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
