package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type endpointLiveShares struct {
	interfaces.KBShareService
	allowed bool
}

func (s *endpointLiveShares) CheckTenantKBPermission(_ context.Context, id string, tenant uint64, _ types.TenantRole) (types.OrgMemberRole, bool, error) {
	return types.OrgRoleViewer, s.allowed && tenant == 1 && id == "shared", nil
}
func TestMCPEndpointSharedSaveRevalidation(t *testing.T) {
	repo := newStubMCPEndpointRepo()
	shares := &endpointLiveShares{allowed: true}
	svc := &mcpEndpointService{repo: repo, kbShareService: shares, kbService: &stubKBForMCPEndpoint{kbs: map[string]*types.KnowledgeBase{
		"own": {ID: "own", TenantID: 1}, "shared": {ID: "shared", TenantID: 2}, "foreign": {ID: "foreign", TenantID: 3},
	}}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "synthetic-admin", Role: types.TenantRoleAdmin})
	ep, token, err := svc.Create(ctx, 1, &types.MCPEndpoint{Name: "shared", Enabled: true, Tools: types.StringArray{"search_knowledge"}, KnowledgeBaseIDs: types.StringArray{"own", "shared", "shared"}})
	if err != nil || len(ep.KnowledgeBaseIDs) != 2 {
		t.Fatalf("live shared and dedup: %v %v", ep, err)
	}
	bad := []string{"foreign"}
	if _, err = svc.Update(ctx, 1, ep.ID, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &bad}); err == nil {
		t.Fatal("forged ID accepted")
	}
	if _, err = svc.validateKnowledgeBases(ctx, 4, []string{"shared"}); err == nil {
		t.Fatal("caller switched workspace")
	}
	shares.allowed = false
	// Authentication is still valid; per-KB authorization is independently revoked.
	if _, err := svc.Authenticate(context.Background(), ep.ID, token); err != nil {
		t.Fatalf("KB share revocation must not alter machine token lifecycle: %v", err)
	}
	selection := []string{"own", "shared"}
	if _, err = svc.Update(ctx, 1, ep.ID, interfaces.MCPEndpointUpdate{KnowledgeBaseIDs: &selection}); err == nil {
		t.Fatal("revoked saved ID accepted on update")
	}
	stored, err := repo.GetByID(ctx, ep.ID)
	if err != nil || len(stored.KnowledgeBaseIDs) != 2 {
		t.Fatalf("failed save overwrote config: %v %v", stored, err)
	}
}

func TestMCPEndpointCreatorDowngradeDoesNotRevokeMachineToken(t *testing.T) {
	svc := newMCPEndpointServiceForTest(newStubMCPEndpointRepo(), nil)
	admin := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "synthetic-creator", Role: types.TenantRoleAdmin})
	ep, token, err := svc.Create(admin, 7, &types.MCPEndpoint{Name: "machine", Enabled: true, Tools: types.StringArray{"search_knowledge"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, caller := range []types.Caller{
		{TenantID: 7, UserID: "synthetic-creator", Role: types.TenantRoleViewer},
		{TenantID: 8, UserID: "synthetic-creator", Role: types.TenantRoleViewer},
		{}, // Creator exits; the machine token authenticates independently of web identity.
	} {
		got, err := svc.Authenticate(types.WithCaller(context.Background(), caller), ep.ID, token)
		if err != nil || got.TenantID != 7 {
			t.Fatalf("creator change incorrectly invalidates/scopes machine token: %v %v", got, err)
		}
	}
	// Administrators can still revoke the machine credential through its existing lifecycle.
	if err := svc.Delete(admin, 7, ep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), ep.ID, token); err == nil {
		t.Fatal("deleted endpoint token still authenticates")
	}
}
