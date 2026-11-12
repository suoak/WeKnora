package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestPersonalContentUsesSeparateRegistryAndAdmissionSlots(t *testing.T) {
	s := ConfigurePersonalContentServer(
		NewPersonalServer(true, &service.PortalMCPKnowledge{}),
		&service.PersonalContent{},
	)
	ctx := types.WithTenantAPIKeyScope(
		types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"}),
		types.TenantAPIKeyScope{
			ScopeType:    types.APIKeyScopeUserMCP,
			Capabilities: types.StringArray{"retrieve", "add_content"},
		},
	)
	schemas := map[bool]map[string]mcp.Tool{}
	list := func(isolated bool, capable bool) []string {
		r := httptest.NewRequest(
			"POST",
			personalPath,
			bytes.NewBufferString(
				`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`),
		)
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
				Tools []mcp.Tool `json:"tools"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		schemas[isolated] = map[string]mcp.Tool{}
		names := []string{}
		for _, tool := range result.Result.Tools {
			names = append(names, tool.Name)
			schemas[isolated][tool.Name] = tool
		}
		return names
	}
	require.Len(t, list(true, true), 8)
	require.NotContains(t, list(true, true), "add_document")
	require.Len(t, list(false, false), 8)
	require.Len(t, list(false, true), 12)
	for name, tool := range schemas[true] {
		require.Equal(t, tool, schemas[false][name], "shared read schema: %s", name)
	}
	for i := 0; i < s.admission.global; i++ {
		release, err := s.admission.acquire(ctx, "busy-"+strconv.Itoa(i))
		require.NoError(t, err)
		defer release()
	}
	// Write admission is independent even when every query slot is occupied.
	r := httptest.NewRequest("POST", personalPath, bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_docume`+
			`nt","arguments":{"knowledge_base_id":"kb","title":"t","content":"body","id`+
			`empotency_key":"request-1"}}}`)).
		WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "choose a current writable")
	require.Equal(t, s.admission.global, s.admission.snapshot("owner").Active)
}

func TestIsolatedPersonalRejectsAllWriteNamesAndHeaderClaims(t *testing.T) {
	s := ConfigurePersonalContentServer(
		NewPersonalServer(true, &service.PortalMCPKnowledge{}),
		&service.PersonalContent{},
	)
	for _, name := range []string{
		"add_document", "upload_document", "get_document_processing_status", "list_writable_knowledge_bases",
		"update_document", "delete_document", "ingest",
	} {
		body, _ := json.Marshal(
			map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params":  map[string]any{"name": name, "arguments": map[string]any{"knowledge_base_id": "kb"}},
			},
		)
		r := httptest.NewRequest("POST", isolatedPersonalPath, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Environment", "external")
		r.Header.Set("X-Isolated", "false")
		r.Header.Set("X-Tenant-ID", "999")
		r = r.WithContext(
			types.WithTenantAPIKeyScope(
				r.Context(),
				types.TenantAPIKeyScope{
					ScopeType:    types.APIKeyScopeUserMCP,
					Capabilities: types.StringArray{"retrieve", "add_content"},
				},
			),
		)
		w := httptest.NewRecorder()
		s.IsolatedHandler().ServeHTTP(w, r)
		require.Equal(t, 400, w.Code, name)
	}
}
