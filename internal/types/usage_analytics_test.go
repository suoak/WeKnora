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

func TestMemoryAndWikiUsageOperationsAreBackground(t *testing.T) {
	operations := []string{
		ModelUsageOperationMemoryExtraction,
		ModelUsageOperationMemoryConsolidation,
		ModelUsageOperationMemoryTopicResolution,
		ModelUsageOperationWikiIngestion,
		ModelUsageOperationWikiGeneration,
		ModelUsageOperationWikiModification,
	}
	for _, operation := range operations {
		if !IsModelUsageOperation(operation) {
			t.Fatalf("operation %q is not registered", operation)
		}
		if class := ModelUsageClass(operation); class != UsageClassBackground {
			t.Fatalf("operation %q class = %q, want background", operation, class)
		}
	}
}

func TestUsageEventKeyHashesSensitiveStableIDs(t *testing.T) {
	key := UsageEventKey(ModelUsageOperationMemoryExtraction, "session-secret", "message-secret")
	if !strings.HasPrefix(key, ModelUsageOperationMemoryExtraction+":") {
		t.Fatalf("unexpected event key: %q", key)
	}
	if strings.Contains(key, "session-secret") || strings.Contains(key, "message-secret") {
		t.Fatalf("event key leaked source identifiers: %q", key)
	}
}
