package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sourceRestrictionService struct {
	interfaces.DataSourceService
	calls int
	err   error
}

func (s *sourceRestrictionService) KnowledgeBaseSourceRestriction(_ context.Context, tenant uint64, kb string) (types.KnowledgeSourceRestriction, error) {
	s.calls++
	return types.KnowledgeSourceRestriction{Restricted: true, SourceState: "hold", HistoricalProcessingState: "unverified"}, s.err
}

func TestSourceRestrictionHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, principal string
		owner           uint64
		err             error
		want            int
	}{
		{"ordinary member", types.PrincipalWebUser, 12, nil, 200},
		{"other tenant", types.PrincipalWebUser, 13, nil, 403},
		{"machine", types.PrincipalAPITenant, 12, nil, 403},
		{"MCP", types.PrincipalMCPEndpoint, 12, nil, 403},
		{"uncertain", types.PrincipalWebUser, 12, errors.New("private credential never expose"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &sourceRestrictionService{err: tc.err}
			kb := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				return &types.KnowledgeBase{ID: "kb", TenantID: tc.owner}, nil
			}}
			h := NewDataSourceHandler(svc, kb)
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(12)); c.Next() })
			r.GET("/knowledge-bases/:id/source-restriction", h.GetKnowledgeBaseSourceRestriction)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(12))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "ordinary-user")
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 12, UserID: "ordinary-user", Role: types.TenantRoleViewer})
			ctx = types.WithPrincipal(ctx, types.Principal{Type: tc.principal, ID: "ordinary-user"})
			req := httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb/source-restriction", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "private credential")
			if tc.want == 200 {
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.Contains(t, w.Body.String(), `"historical_processing_state":"unverified"`)
			}
			if tc.want == 403 {
				require.Zero(t, svc.calls)
			}
		})
	}
}
