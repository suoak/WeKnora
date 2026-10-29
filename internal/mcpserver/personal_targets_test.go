package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type selectedMCPStore struct {
	access.DocumentPolicyStore
	access.EnterprisePolicyStore
	unrelatedReads int
	version        string
	revoked        bool
	held           bool
}

func (s *selectedMCPStore) LoadDocumentReadSnapshot(_ context.Context, user string, key uint64, kb string) (*types.EnterpriseReadSnapshot, error) {
	if kb != "selected" {
		s.unrelatedReads++
		return nil, errors.New("unrelated source unavailable")
	}
	if s.held {
		return nil, types.ErrLegacySourceRestricted
	}
	owner := "A"
	v := &types.EnterpriseReadSnapshot{
		UserID: user, UserActive: true, TenantID: 99, TenantActive: true,
		KBID: kb, KBType: "document",
		Credential: types.EnterpriseCredential{ID: key, OwnerUserID: &owner, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve"}},
		Documents:  []types.EnterpriseDocumentState{{ID: "doc", TenantID: 99, KnowledgeBaseID: kb, Type: "file", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CurrentVersion: s.version}},
	}
	if s.revoked {
		now := time.Now().UTC()
		v.Credential.RevokedAt = &now
	}
	return v, nil
}

type selectedMCPDirectory struct{}

func (selectedMCPDirectory) ListPortalMCPCandidates(context.Context) ([]access.PortalKBDescriptor, error) {
	return []access.PortalKBDescriptor{{ID: "selected", TenantID: 99}, {ID: "unrelated", TenantID: 99}}, nil
}

type selectedMCPEmployee struct{}

func (selectedMCPEmployee) IsEnterpriseEmployee(_ context.Context, user string) (bool, error) {
	return user == "A", nil
}

type selectedMCPBase struct{}

func (selectedMCPBase) CheckDocumentReadBase(ctx context.Context, tenant uint64, kb string, _ types.DocumentOperation, _ bool) error {
	key, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || !key.AllowsKnowledgeBase(kb) || tenant != 99 || types.CallerFromContext(ctx).UserID != "A" {
		return access.ErrForbidden
	}
	return nil
}

func selectedMCPFixture() (*PersonalServer, *selectedMCPStore, context.Context) {
	store := &selectedMCPStore{version: "v1"}
	employee := selectedMCPEmployee{}
	resolver := &access.PortalMCPResolver{
		Enabled: true, PublishedDefaults: true, Directory: selectedMCPDirectory{}, Store: store, Employees: employee,
		Public:    access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true, PortalPublishedDefault: true}, store, employee, nil),
		Documents: access.NewDocumentReader(true, store, selectedMCPBase{}, employee),
	}
	ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "A"})
	ctx = types.WithCaller(ctx, types.Caller{UserID: "A", TenantID: 99, Role: types.TenantRoleAdmin})
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve"}, KnowledgeBaseRestricted: true, KnowledgeBaseIDs: types.StringArray{"selected"}})
	return NewPersonalServer(true, &service.PortalMCPKnowledge{Resolver: resolver}), store, ctx
}

func selectedMCPRequest(t *testing.T, args map[string]any) mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"params": map[string]any{"name": "search_knowledge", "arguments": args}})
	require.NoError(t, err)
	var request mcp.CallToolRequest
	require.NoError(t, json.Unmarshal(raw, &request))
	return request
}

func TestPersonalMCPSelectedReadIgnoresUnrelatedSourceFailure(t *testing.T) {
	s, store, ctx := selectedMCPFixture()
	called := false
	handler := s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return mcp.NewToolResultText("selected result"), nil
	})
	result, err := handler(ctx, selectedMCPRequest(t, map[string]any{"knowledge_base_ids": []string{"selected"}}))
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.True(t, called)
	require.Zero(t, store.unrelatedReads, "unrequested sources must not enter the read proof")
}

func TestPersonalMCPSelectedReadStillDiscardsStaleOrRevokedOutput(t *testing.T) {
	for _, mutation := range []string{"key-revoked", "version-changed", "source-held"} {
		t.Run(mutation, func(t *testing.T) {
			s, store, ctx := selectedMCPFixture()
			handler := s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				switch mutation {
				case "key-revoked":
					store.revoked = true
				case "version-changed":
					store.version = "v2"
				case "source-held":
					store.held = true
				}
				return mcp.NewToolResultText("MUST_NOT_ESCAPE"), nil
			})
			result, err := handler(ctx, selectedMCPRequest(t, map[string]any{"knowledge_base_ids": []string{"selected"}}))
			require.NoError(t, err)
			require.True(t, result.IsError)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "MUST_NOT_ESCAPE")
			require.Zero(t, store.unrelatedReads)
		})
	}
}

func TestPersonalMCPBadSelectionNeverFallsBackToAll(t *testing.T) {
	for _, ids := range []any{"selected", []any{1}, []any{""}, []any{"selected", nil}, []any{"not-granted"}} {
		s, store, ctx := selectedMCPFixture()
		called := false
		handler := s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			called = true
			return mcp.NewToolResultText("MUST_NOT_ESCAPE"), nil
		})
		result, err := handler(ctx, selectedMCPRequest(t, map[string]any{"knowledge_base_ids": ids}))
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.False(t, called)
		require.Zero(t, store.unrelatedReads)
	}
}

func TestPersonalMCPHTTPAdmissionUsesToolSelection(t *testing.T) {
	s, store, ctx := selectedMCPFixture()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"q","knowledge_base_ids":["selected"]}}}`
	req := httptest.NewRequest("POST", "/api/v1/mcp/personal", bytes.NewBufferString(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	// The fixture deliberately has no retrieval engine. Admission must still
	// reach the SDK's tool result instead of failing on the unrelated source.
	require.Equal(t, 200, w.Code)
	require.Zero(t, store.unrelatedReads)
	require.Contains(t, w.Body.String(), "resource denied or unavailable")
}

func TestPersonalMCPHTTPAdmissionRejectsInvalidSelection(t *testing.T) {
	s, store, ctx := selectedMCPFixture()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"knowledge_base_ids":[42]}}}`
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mcp/personal", bytes.NewBufferString(body)).WithContext(ctx))
	require.Equal(t, 400, w.Code)
	require.Zero(t, store.unrelatedReads)
}
