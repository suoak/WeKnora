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

func TestSteadyHTTPChecksIndependentTargetAuthority(t *testing.T) {
	anchor, target := uuid.NewString(), uuid.NewString()
	feishucontinuity.Activate(t, anchor, func(_ context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		return feishusynccontrol.RecoveryFacts{
			SourceID: id, Eligible: true, SelectedResourceCount: 1,
			ConfigSHA256: strings.Repeat("b", 64), FactsSHA256: strings.Repeat("c", 64),
		}, nil
	})
	allowed := false
	calls := 0
	m := feishusynccontrol.Recovery()
	m.ConfigureSteadyAuthority(func(
		_ context.Context, id, actor string,
	) (feishusynccontrol.SourceManagementAuthority, error) {
		calls++
		a := feishusynccontrol.SourceManagementAuthority{
			SourceID: id, TenantID: 2, KBID: uuid.NewString(),
			SourceStatus: "active", AccessState: "unmanaged",
			SourceType: "feishu", ActorID: actor, AuthorityHash: strings.Repeat("e", 64),
		}
		if !allowed {
			return a, feishusynccontrol.ErrRecoveryRejected
		}
		return a, nil
	})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "fixture-operator")
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "fixture-operator"})
	sources := &stubDataSourceService{getDataSource: func(context.Context, string) (*types.DataSource, error) {
		return &types.DataSource{ID: anchor, KnowledgeBaseID: anchor}, nil
	}}
	kbs := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		return &types.KnowledgeBase{ID: anchor, TenantID: 1}, nil
	}}
	h := NewDataSourceHandler(sources, kbs)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(1)) })
	r.POST("/:id/steady/:operation", h.FeishuSteadySync)
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/"+anchor+"/steady/plan", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	body := `{"sources":["` + target + `"],"authorization_days":7}`
	require.Equal(t, 409, call(body).Code)
	require.Positive(t, calls, "anchor admin still requires independent target authorization")
	allowed = true
	require.Equal(t, 200, call(body).Code, "a genuinely authorized different tenant must be selectable")
	forged := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1})
	ctx = forged
	require.Equal(t, 403, call(body).Code, "machine keys never become Web approval actors")
}
