package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecoveryHTTPAuthorizationBeforePlanOrExecution(t *testing.T) {
	for _, scenario := range []string{
		"ordinary_user", "machine_key", "cross_tenant", "private_owner_denied", "disabled",
	} {
		t.Run(scenario, func(t *testing.T) {
			feishusynccontrol.InstallRecovery(nil)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "operator")
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, scenario != "ordinary_user")
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "operator"})
			if scenario == "machine_key" {
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 9})
			}
			sourceCalls := 0
			service := &stubDataSourceService{
				getDataSource: func(context.Context, string) (*types.DataSource, error) {
					sourceCalls++
					return &types.DataSource{ID: "source", KnowledgeBaseID: "kb"}, nil
				},
			}
			kbs := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				if scenario == "private_owner_denied" {
					return nil, types.ErrPrivateKnowledgeBase
				}
				tenant := uint64(1)
				if scenario == "cross_tenant" {
					tenant = 2
				}
				return &types.KnowledgeBase{ID: "kb", TenantID: tenant}, nil
			}}
			h := NewDataSourceHandler(service, kbs)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(1)) })
			router.GET("/recovery/:id", h.FeishuRecoveryPlan)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/recovery/source", nil).WithContext(ctx)
			router.ServeHTTP(w, req)
			switch scenario {
			case "disabled":
				require.Equal(t, 503, w.Code)
			case "private_owner_denied":
				require.Equal(t, 404, w.Code)
			default:
				require.Equal(t, 403, w.Code)
			}
			if scenario == "ordinary_user" || scenario == "machine_key" {
				require.Zero(t, sourceCalls)
			}
		})
	}
}
