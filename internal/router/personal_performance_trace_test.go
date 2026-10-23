package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalPerformanceTraceUsesServerCorrelationID(t *testing.T) {
	r := gin.New()
	r.GET("/", personalPerformanceTrace, func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-KnowHub-Perf-ID", "caller-supplied-identity")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	id := response.Header().Get("X-KnowHub-Perf-ID")
	require.Len(t, id, 32)
	require.NotEqual(t, "caller-supplied-identity", id)
	require.Empty(t, response.Body.String())
}
