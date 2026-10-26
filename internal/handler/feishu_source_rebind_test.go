package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSourceRebindRequiresAdminAndSeparateOperationEnablement(t *testing.T) {
	t.Setenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED", "")
	for _, action := range []string{"prepare", "authorize-publication", "apply", "rollback"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "action", Value: action}}
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		(&FeishuPersonalHandler{}).SourceRebind(c)
		require.Equal(t, http.StatusForbidden, w.Code)
		if action == "prepare" {
			continue
		}
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "action", Value: action}}
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), types.SystemAdminContextKey, true),
		)
		(&FeishuPersonalHandler{}).SourceRebind(c)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Contains(t, w.Body.String(), "source_rebind_operations_disabled")
	}
}
