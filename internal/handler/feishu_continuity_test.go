package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/testutil/feishucontinuity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApprovedSourcesHTTPRequiresEverySourceAuthority(t *testing.T) {
	for _, scenario := range []string{"cross_tenant", "private_owner_denied"} {
		t.Run(scenario, func(t *testing.T) {
			id, other := uuid.NewString(), uuid.NewString()
			feishucontinuity.Activate(
				t,
				id,
				func(_ context.Context, source string) (feishusynccontrol.RecoveryFacts, error) {
					return feishusynccontrol.RecoveryFacts{
						SourceID: source, Eligible: true, SelectedResourceCount: 1,
						ConfigSHA256: strings.Repeat("b", 64), FactsSHA256: strings.Repeat("c", 64),
					}, nil
				},
			)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "operator")
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "operator"})
			service := &stubDataSourceService{
				getDataSource: func(_ context.Context, source string) (*types.DataSource, error) {
					return &types.DataSource{ID: source, KnowledgeBaseID: source}, nil
				},
			}
			kbs := &stubKBServiceForDS{getByID: func(_ context.Context, kb string) (*types.KnowledgeBase, error) {
				tenant := uint64(1)
				if kb == other {
					if scenario == "private_owner_denied" {
						return nil, types.ErrPrivateKnowledgeBase
					}
					tenant = 2
				}
				return &types.KnowledgeBase{ID: kb, TenantID: tenant}, nil
			}}
			h := NewDataSourceHandler(service, kbs)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(1)) })
			router.POST("/:id/continuity/:operation", h.FeishuContinuity)
			for _, operation := range []string{"plan", "activate"} {
				body := `{"sources":["` + id + `","` + other + `"]}`
				if operation == "activate" {
					body = `{"plan":{"sources":[{"source_id":"` + id + `"},{"source_id":"` + other + `"}]}}`
				}
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/"+id+"/continuity/"+operation,
					strings.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(w, req)
				require.Equal(t, 403, w.Code, operation)
				require.Contains(t, w.Body.String(), "continuity_source_forbidden")
			}
		})
	}
}
