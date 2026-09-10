package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseUsageQueryPhase2Filters(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/?operation=query_rewrite&usage_class=background&channel=background&model_type=knowledge_qa&direction=outbound", nil)
	query, err := parseUsageQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if query.Operation != "query_rewrite" || query.UsageClass != "background" || query.Channel != "background" ||
		query.ModelType != "knowledge_qa" || query.Direction != "outbound" {
		t.Fatalf("filters not parsed: %#v", query)
	}
}

func TestParseUsageQueryRejectsUnknownClassification(t *testing.T) {
	for _, raw := range []string{"operation=free-text", "usage_class=system", "direction=sideways", "status=stale", "group_by=arbitrary", "metric=cost", "dimension=prompt", "include_inactive=perhaps"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", "/?"+raw, nil)
		if _, err := parseUsageQuery(ctx); err == nil {
			t.Fatalf("accepted invalid filter %q", raw)
		}
	}
}

func TestParseUsageGovernanceFilters(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/?group_by=knowledge_base&status=inactive&include_inactive=true&mcp_adoption=adopted&cross_tenant=with&owner_tenant_id=9&knowledge_base_id=kb-1&metric=accesses&dimension=scope", nil)
	query, err := parseUsageQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if query.GroupBy != "knowledge_base" || query.Status != "inactive" || !query.IncludeInactive || query.MCPAdoption != "adopted" || query.CrossTenant != "with" || query.OwnerTenantID == nil || *query.OwnerTenantID != 9 || query.KnowledgeBaseID != "kb-1" || query.Metric != "accesses" || query.Dimension != "scope" {
		t.Fatalf("governance filters not parsed: %#v", query)
	}
}
