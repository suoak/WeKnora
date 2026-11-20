package mcpserver

import (
	"context"
	"errors"
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
	for _, name := range personalToolCatalog[8:] {
		for _, tc := range []struct {
			label string
			cause error
			code  string
		}{
			{"success", nil, ""}, {"permission", access.ErrForbidden, "permission_denied"},
			{"database", errors.New("database failed"), "execution_error"},
			{"arguments", service.ErrPersonalContentArguments, "invalid_arguments"},
			{"timeout", context.DeadlineExceeded, "timeout"}, {"cancel", context.Canceled, "canceled"},
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
