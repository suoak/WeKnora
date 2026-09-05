package handler

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFilterKnowledgeBasesForAPIKeyScopeDeniesExplicitEmptyRestriction(t *testing.T) {
	kbs := []*types.KnowledgeBase{{ID: "kb-1"}, {ID: "kb-2"}}
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KnowledgeBaseRestricted: true,
	})

	if got := filterKnowledgeBasesForAPIKeyScope(ctx, kbs); len(got) != 0 {
		t.Fatalf("filtered knowledge bases = %#v, want none", got)
	}
}

func TestFilterKnowledgeBasesForAPIKeyScopeKeepsOnlyAllowedIDs(t *testing.T) {
	kbs := []*types.KnowledgeBase{{ID: "kb-1"}, {ID: "kb-2"}}
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KnowledgeBaseRestricted: true,
		KnowledgeBaseIDs:        types.StringArray{"kb-2"},
	})

	got := filterKnowledgeBasesForAPIKeyScope(ctx, kbs)
	if len(got) != 1 || got[0].ID != "kb-2" {
		t.Fatalf("filtered knowledge bases = %#v, want kb-2", got)
	}
}

func TestFilterKnowledgeBasesForAPIKeyScopeLeavesUnrestrictedCallerUntouched(t *testing.T) {
	kbs := []*types.KnowledgeBase{{ID: "kb-1"}}
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{})

	got := filterKnowledgeBasesForAPIKeyScope(ctx, kbs)
	if len(got) != 1 || got[0] != kbs[0] {
		t.Fatalf("filtered knowledge bases = %#v, want original list", got)
	}
}
