package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalUsageCatalogMatchesExternalRegistry(t *testing.T) {
	s := ConfigurePersonalContentServer(
		NewPersonalServer(true, &service.PortalMCPKnowledge{}), &service.PersonalContent{},
	)
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		Capabilities: types.StringArray{"add_content"},
	})
	r := httptest.NewRequest("POST", personalPath,
		bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.externalHandler.ServeHTTP(w, r)
	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	names := []string{}
	for _, tool := range response.Result.Tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, personalToolCatalog[:], names)
	for _, name := range names {
		require.Equal(t, name, newUsageReport("go_personal", name).ToolName)
	}
	require.Equal(t, "chat", newUsageReport("go_personal", "chat").ToolName)
	require.False(t, knownPersonalTool("chat"))
}

func TestPersonalContentHTTPUsageSingleFinalEvent(t *testing.T) {
	for _, name := range personalToolCatalog[8:] {
		t.Run(name, func(t *testing.T) {
			sink := &captureUsage{}
			s := ConfigurePersonalContentServer(
				NewPersonalServer(true, &service.PortalMCPKnowledge{}), &service.PersonalContent{},
			)
			s.SetUsageRecorder(sink) // recorder attached after registry construction
			ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"})
			body, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": name, "arguments": map[string]any{}},
			})
			r := httptest.NewRequest("POST", personalPath, bytes.NewReader(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"isError":true`)
			require.Len(t, sink.reports, 1)
			require.Equal(t, name, sink.reports[0].ToolName)
			require.False(t, sink.reports[0].Success)
		})
	}
}
