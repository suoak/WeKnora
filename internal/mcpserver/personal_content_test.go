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

func TestPersonalContentUsesSeparateRegistryAndAdmissionSlots(t *testing.T) {
	s := ConfigurePersonalContentServer(NewPersonalServer(true, &service.PortalMCPKnowledge{}), &service.PersonalContent{})
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve", "add_content"}})
	list := func(isolated bool, capable bool) []string {
		r := httptest.NewRequest("POST", personalPath, bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
		if capable {
			r = r.WithContext(ctx)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		if isolated {
			s.handler.ServeHTTP(w, r)
		} else {
			s.externalHandler.ServeHTTP(w, r)
		}
		require.Equal(t, 200, w.Code)
		var result struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		names := []string{}
		for _, tool := range result.Result.Tools {
			names = append(names, tool.Name)
		}
		return names
	}
	require.Len(t, list(true, true), 8)
	require.NotContains(t, list(true, true), "add_document")
	require.Len(t, list(false, false), 8)
	require.Len(t, list(false, true), 12)
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	// Write admission is independent even when every query slot is occupied.
	r := httptest.NewRequest("POST", personalPath, bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_document","arguments":{"knowledge_base_id":"kb","title":"t","content":"body","idempotency_key":"request-1"}}}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "choose a current writable")
	require.Equal(t, cap(s.slots), len(s.slots))
}

func TestIsolatedPersonalRejectsAllWriteNamesAndHeaderClaims(t *testing.T) {
	s := ConfigurePersonalContentServer(NewPersonalServer(true, &service.PortalMCPKnowledge{}), &service.PersonalContent{})
	for _, name := range []string{"add_document", "upload_document", "get_document_processing_status", "list_writable_knowledge_bases", "update_document", "delete_document", "ingest"} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{"knowledge_base_id": "kb"}}})
		r := httptest.NewRequest("POST", isolatedPersonalPath, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Environment", "external")
		r.Header.Set("X-Isolated", "false")
		r.Header.Set("X-Tenant-ID", "999")
		r = r.WithContext(types.WithTenantAPIKeyScope(r.Context(), types.TenantAPIKeyScope{ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve", "add_content"}}))
		w := httptest.NewRecorder()
		s.IsolatedHandler().ServeHTTP(w, r)
		require.Equal(t, 400, w.Code, name)
	}
}
