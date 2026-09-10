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
	for _, raw := range []string{"operation=free-text", "usage_class=system", "direction=sideways"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", "/?"+raw, nil)
		if _, err := parseUsageQuery(ctx); err == nil {
			t.Fatalf("accepted invalid filter %q", raw)
		}
	}
}
