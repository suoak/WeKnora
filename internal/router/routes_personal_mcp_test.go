package router

import (
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPortalM6DisabledRouteDoesNotAuthenticateOrTouchKey(t *testing.T) {
	r := gin.New()
	// Nil dependencies make any attempted Auth or last_used write fail loudly.
	RegisterPersonalMCPRoutes(r, mcpserver.NewDisabledPersonalServer(), nil, nil, nil, nil, nil)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		req := httptest.NewRequest(method, "/api/v1/mcp/personal", nil)
		req.Header.Set("Authorization", "Bearer synthetic-invalid")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		require.Equal(t, 403, out.Code)
	}
}
