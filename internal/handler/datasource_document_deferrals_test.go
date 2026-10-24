package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type documentDeferralHTTPService struct {
	stubDataSourceService
	replace func(context.Context, string, string, []types.SyncDocumentDeferral) (*types.DataSource, error)
}

func (s *documentDeferralHTTPService) ReplaceDocumentDeferrals(ctx context.Context,
	id,
	hash string,
	entries []types.SyncDocumentDeferral) (*types.DataSource,
	error,
) {
	return s.replace(ctx, id, hash, entries)
}

func TestDocumentDeferralHTTPIdentityAndConflict(t *testing.T) {
	for _, scenario := range []string{
		"get",
		"replace",
		"missing_tenant",
		"cross_tenant",
		"hold_diagnostics",
		"stale_policy",
		"missing_hash",
	} {
		t.Run(scenario, func(t *testing.T) {
			source := &types.DataSource{
				ID:              "ds",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Config:          types.JSON(`{"resource_ids":["root"]}`),
			}
			called := false
			svc := &documentDeferralHTTPService{stubDataSourceService: stubDataSourceService{
				getDataSource: func(context.Context, string) (*types.DataSource, error) { return source, nil },
			}}
			svc.replace = func(
				_ context.Context, id, hash string, entries []types.SyncDocumentDeferral,
			) (*types.DataSource, error) {
				called = true
				require.Equal(t, "ds", id)
				require.Equal(t, types.DocumentDeferralHash(nil), hash)
				require.Empty(t, entries)
				if scenario == "stale_policy" {
					return nil, errors.New("document policy changed; reload before editing")
				}
				return source, nil
			}
			kbCalls := 0
			kb := &stubKBServiceForDS{getByID: func(ctx context.Context, _ string) (*types.KnowledgeBase, error) {
				kbCalls++
				if scenario == "hold_diagnostics" && kbCalls > 1 {
					return nil, errors.New("hold")
				}
				return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
			}}
			h := NewDataSourceHandler(svc, kb)
			r := newDataSourceTestRouter(h)
			r.GET("/datasource/:id/document-deferrals", h.GetDocumentDeferrals)
			r.PUT("/datasource/:id/document-deferrals", h.PutDocumentDeferrals)
			method := http.MethodGet
			body := ""
			if scenario == "replace" || scenario == "stale_policy" || scenario == "missing_hash" {
				method = http.MethodPut
				body = `{"expected_policy_hash":"` + types.DocumentDeferralHash(nil) + `","documents":[]}`
			}
			if scenario == "missing_hash" {
				body = `{"documents":[]}`
			}
			req := httptest.NewRequest(method, "/datasource/ds/document-deferrals", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if scenario != "missing_tenant" {
				tenant := uint64(1)
				if scenario == "cross_tenant" {
					tenant = 2
				}
				req = withDSCtx(req, tenant)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			expected := http.StatusOK
			switch scenario {
			case "missing_tenant":
				expected = http.StatusUnauthorized
			case "cross_tenant", "hold_diagnostics":
				expected = http.StatusForbidden
			case "stale_policy":
				expected = http.StatusConflict
			case "missing_hash":
				expected = http.StatusBadRequest
			}
			require.Equal(t, expected, w.Code, w.Body.String())
			require.Equal(t, scenario == "replace" || scenario == "stale_policy", called)
		})
	}
}
