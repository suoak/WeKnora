package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// Real service/database authorization and idempotency are independently covered
// by service tests. This exercises the registered handler's result/usage boundary.
type contentFixture struct {
	cause  error
	status string
}

func (f contentFixture) ListWritable(context.Context) ([]service.WritableKnowledgeBase, error) {
	return []service.WritableKnowledgeBase{{ID: "kb"}}, f.cause
}

func (f contentFixture) ResolveContentSelector(context.Context, string) (string, error) {
	return "kb", nil
}

func (f contentFixture) row() (*types.Knowledge, error) {
	return &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", ParseStatus: f.status}, f.cause
}

func (f contentFixture) Add(context.Context, service.PersonalContentRequest) (*types.Knowledge, error) {
	return f.row()
}

func (f contentFixture) Upload(context.Context, service.PersonalUploadRequest) (*types.Knowledge, error) {
	return f.row()
}
func (f contentFixture) Status(context.Context, string) (*types.Knowledge, error) { return f.row() }

func TestPersonalContentResultUsageMatrix(t *testing.T) {
	for _, name := range personalToolCatalog[10:] {
		for _, tc := range []struct {
			label string
			cause error
			code  string
		}{
			{"success", nil, ""},
			{"permission", access.ErrForbidden, "permission_denied"},
			{"database", errors.New("database failed"), "execution_error"},
			{"arguments", service.ErrPersonalContentArguments, "invalid_arguments"},
			{"timeout", context.DeadlineExceeded, "timeout"},
			{"cancel", context.Canceled, "canceled"},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				sink := &captureUsage{}
				s := &PersonalServer{content: contentFixture{cause: tc.cause, status: "processing"}, usage: sink}
				req := mcp.CallToolRequest{}
				req.Params.Name = name
				args := map[string]any{}
				switch name {
				case "add_document":
					args = map[string]any{
						"knowledge_base_id": "kb", "title": "title", "content": "body", "idempotency_key": "repeat-1",
					}
				case "upload_document":
					args = map[string]any{
						"knowledge_base_id": "kb", "filename": "x.txt",
						"content_base64": "Ym9keQ==", "idempotency_key": "repeat-1",
					}
				case "get_document_processing_status":
					args = map[string]any{"document_id": "doc"}
				}
				req.Params.Arguments = args
				ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
					Capabilities: types.StringArray{"add_content"},
				})
				result, err := s.observeContent()(ctx, req)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, sink.reports, 1)
				require.Equal(t, tc.cause != nil, result.IsError)
				require.Equal(t, tc.cause == nil, sink.reports[0].Success)
				require.Equal(t, tc.code, sink.reports[0].ErrorCode)
			})
		}
	}
}

func TestPersonalProcessingFailureIsSuccessfulStatusRead(t *testing.T) {
	result, err := personalProcessingResult(context.Background(), &types.Knowledge{ID: "doc", ParseStatus: "failed"})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Empty(t, usageError(context.Background(), result, nil))
}

func TestPersonalEightMiBUploadObserved(t *testing.T) {
	sink := &captureUsage{}
	s := ConfigurePersonalContentServer(
		NewPersonalServer(true, &service.PortalMCPKnowledge{}), &service.PersonalContent{},
	)
	s.content = contentFixture{status: "processing"}
	s.SetUsageRecorder(sink)
	data := bytes.Repeat([]byte("a"), service.PersonalUploadMaxBytes)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "upload_document", "arguments": map[string]any{
			"knowledge_base_id": "kb", "filename": "maximum.txt",
			"content_base64": base64.StdEncoding.EncodeToString(data), "idempotency_key": "maximum-1",
		}},
	})
	require.NoError(t, err)
	require.Greater(t, len(body), 8<<20)
	require.Less(t, len(body), 12<<20)
	ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"})
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{Capabilities: types.StringArray{"add_content"}})
	r := httptest.NewRequest("POST", personalPath, bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response.Result.Content)
	require.Contains(t, response.Result.Content[0].Text, `"status":"processing"`)
	require.Len(t, sink.reports, 1)
	require.True(t, sink.reports[0].Success)
	require.Equal(t, "upload_document", sink.reports[0].ToolName)
}
