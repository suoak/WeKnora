package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type documentReparseServiceStub struct {
	stubDataSourceService
	request func(context.Context, string, string, *types.KnowledgeProcessOverrides) (
		*types.FeishuDocumentReparseStatus, error,
	)
}

func (s *documentReparseServiceStub) RequestDocumentReparse(
	ctx context.Context, source, kid string, config *types.KnowledgeProcessOverrides,
) (*types.FeishuDocumentReparseStatus, error) {
	return s.request(ctx, source, kid, config)
}

func (*documentReparseServiceStub) DocumentReparseStatus(
	context.Context, string, string,
) (*types.FeishuDocumentReparseStatus, error) {
	return nil, nil
}

func TestSourceDocumentReparseHandler(t *testing.T) {
	for _, tenant := range []uint64{1, 2} {
		t.Run(fmt.Sprint(tenant), func(t *testing.T) {
			source, kid := uuid.NewString(), uuid.NewString()
			calls := 0
			service := &documentReparseServiceStub{
				stubDataSourceService: stubDataSourceService{getDataSource: func(context.Context, string) (
					*types.DataSource, error,
				) {
					return &types.DataSource{ID: source, TenantID: 1, KnowledgeBaseID: "kb"}, nil
				}},
				request: func(_ context.Context, gotSource, gotKID string, config *types.KnowledgeProcessOverrides) (
					*types.FeishuDocumentReparseStatus, error,
				) {
					calls++
					require.Equal(t, source, gotSource)
					require.Equal(t, kid, gotKID)
					require.Nil(t, config, "omitted settings must preserve existing document settings")
					return &types.FeishuDocumentReparseStatus{
						ID: uuid.NewString(), KnowledgeID: kid, State: "queued",
					}, nil
				},
			}
			kb := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
			}}
			h := NewDataSourceHandler(service, kb)
			router := newDataSourceTestRouter(h)
			router.POST("/datasource/:id/documents/:knowledge_id/reparse", h.ReparseSourceDocument)
			// These fields cannot become connector identity or publication authority.
			body := `{"root_id":"injected","external_id":"injected","execution_generation":999,"nonce":"injected"}`
			path := "/datasource/" + source + "/documents/" + kid + "/reparse"
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, withDSCtx(req, tenant))
			if tenant == 1 {
				require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
				require.Equal(t, 1, calls)
				require.Contains(t, w.Body.String(), `"state":"queued"`)
				require.NotContains(t, w.Body.String(), "injected")
			} else {
				require.Equal(t, http.StatusForbidden, w.Code)
				require.Zero(t, calls, "cross-tenant requests must not enqueue work")
			}
		})
	}
}
