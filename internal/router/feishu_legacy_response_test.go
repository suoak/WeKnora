package router

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type legacyResponseTestGate struct{ denied bool }

type authenticatedLegacyGate struct{}

func (authenticatedLegacyGate) CheckLegacyReadScope(ctx context.Context) error {
	if types.WorkspaceWebReadTenant(ctx) != 1 {
		return types.ErrLegacySourceRestricted
	}
	return nil
}

func TestLegacyResponseUsesAuthenticatedRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(legacyReadScopeMiddleware(authenticatedLegacyGate{}))
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 1, UserID: "member", Role: types.TenantRoleViewer})
		c.Request = c.Request.WithContext(types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "member"}))
		c.Next()
	})
	r.GET("/", func(c *gin.Context) { c.JSON(200, gin.H{"documents": 266}) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"documents":266`)
}

func (g *legacyResponseTestGate) CheckLegacyReadScope(context.Context) error {
	if g.denied {
		return types.ErrLegacySourceRestricted
	}
	return nil
}
func TestLegacyFileResponseBoundaryStopsWithdrawnContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, streamed := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		gate := &legacyResponseTestGate{}
		writer := &legacyScopedResponse{ResponseWriter: ctx.Writer, ctx: context.Background(), gate: gate}
		if streamed {
			_, err := writer.WriteString("previously authorized prefix")
			require.NoError(t, err)
		}
		gate.denied = true
		if !streamed {
			writer.Header().Set("Content-Length", "999999")
		}
		n, err := writer.WriteString("withdrawn-sensitive-canary")
		require.Zero(t, n)
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
		require.NotContains(t, recorder.Body.String(), "withdrawn-sensitive-canary")
		if !streamed {
			require.Equal(t, 403, recorder.Code)
			require.Empty(t, recorder.Header().Get("Content-Length"), "deny must not retain the old file length and stall clients")
		}
	}
}
