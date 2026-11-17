package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSystemConfluenceCredentialsCannotBeMutated(t *testing.T) {
	svc := &stubDataSourceService{getDataSource: func(context.Context, string) (*types.DataSource, error) {
		return &types.DataSource{ID: "managed", Type: types.ConnectorTypeConfluence, KnowledgeBaseID: "kb", Config: types.JSON(`{"settings":{"credential_source":"system"}}`)}, nil
	}}
	kb := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
	}}
	router := newDataSourceTestRouter(NewDataSourceHandler(svc, kb))
	h := NewDataSourceCredentialsHandler(svc, kb)
	router.PUT("/datasource/:id/credentials", h.Put)
	router.DELETE("/datasource/:id/credentials/:field", h.DeleteField)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		path := "/datasource/managed/credentials"
		if method == http.MethodDelete {
			path += "/credentials"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{"credentials":{"password":"replacement"}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, withDSCtx(req, 1))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403, got %d", method, w.Code)
		}
	}
}
