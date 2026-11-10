package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalMCPLoggerNeverPersistsPayloadEvenOnRefusal(t *testing.T) {
	const secret = "SYNTHETIC_PERSONAL_QUERY_KEY_FILE_URL_CANARY"
	var output bytes.Buffer
	logger.SetOutput(&output)
	defer logger.SetOutput(os.Stdout)
	g := gin.New()
	g.ContextWithFallback = true
	g.Use(RequestID(), Logger())
	g.POST("/api/v1/mcp/personal", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), secret)
		require.NotEqual(t, secret, c.GetString("request_id"))
		logger.Errorf(c.Request.Context(), "upstream echoed %s", secret)
		_ = c.Error(fmt.Errorf("tool error %s", secret))
		c.JSON(403, gin.H{"content": secret})
	})
	r := httptest.NewRequest("POST", "/api/v1/mcp/personal?query="+secret, strings.NewReader(`{"query":"`+secret+`"}`))
	r.Header.Set("X-Request-ID", secret)
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
	require.NotContains(t, output.String(), secret)
	require.Contains(t, output.String(), "personal MCP request")
}

func TestPersonalMCPPanicDoesNotReturnInternalPaths(t *testing.T) {
	g := gin.New()
	g.ContextWithFallback = true
	g.Use(RequestID(), Logger(), Recovery(), ErrorHandler())
	g.POST("/api/v1/mcp/personal", func(c *gin.Context) { panic("SYNTHETIC_PRIVATE_ERROR /home/private secret") })
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mcp/personal", nil))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "SYNTHETIC_PRIVATE_ERROR")
	require.NotContains(t, w.Body.String(), "/home/private")
}
