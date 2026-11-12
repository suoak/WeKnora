package router

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalPerformanceMetricsSurvivePayloadPrivacy(t *testing.T) {
	var output bytes.Buffer
	previous := logger.GetLogger(context.Background()).Logger.Out
	logger.SetOutput(&output)
	defer logger.SetOutput(previous)
	g := gin.New()
	g.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(logger.WithPrivatePayload(c.Request.Context()))
		c.Next()
	})
	g.POST("/api/v1/mcp/personal", personalPerformanceTrace, func(c *gin.Context) {
		ctx := c.Request.Context()
		perftrace.Authenticated(ctx)
		perftrace.Admission(ctx, "tool", "admitted", time.Millisecond,
			perftrace.AdmissionLoad{Active: 3, UserActive: 3})
		perftrace.AdmissionReleased(ctx, perftrace.AdmissionLoad{Active: 2, UserActive: 2})
		logger.Infof(ctx, "SECRET_PAYLOAD_MARKER")
		c.Status(200)
	})
	r := httptest.NewRequest("POST", "/api/v1/mcp/personal", strings.NewReader("SECRET_BODY_MARKER"))
	r.Header.Set("Authorization", "Bearer SECRET_KEY_MARKER")
	g.ServeHTTP(httptest.NewRecorder(), r)
	require.Equal(t, 1, strings.Count(output.String(), "[PersonalMCPPerf]"))
	require.Contains(t, output.String(), `"after_release":{"active":2`)
	for _, secret := range []string{"SECRET_PAYLOAD_MARKER", "SECRET_BODY_MARKER", "SECRET_KEY_MARKER"} {
		require.NotContains(t, output.String(), secret)
	}
}
