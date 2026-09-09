package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPUsageReportJSONCannotCarryAttributionOrSecrets(t *testing.T) {
	payload, err := json.Marshal(MCPUsageReport{
		EventKey: "mcp:1", ToolName: "search", SharedGateway: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"shared_gateway", "caller_tenant", "resource_tenant", "api_key", "authorization", "query", "result"} {
		if strings.Contains(strings.ToLower(serialized), forbidden) {
			t.Fatalf("safe MCP report contains %q: %s", forbidden, serialized)
		}
	}
}
