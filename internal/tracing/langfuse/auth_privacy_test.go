package langfuse

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOIDCTracingExcludesCredentials(t *testing.T) {
	_, exp := newTestManager(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	for _, path := range []string{
		"/api/v1/auth/oidc/callback",
		"/api/v1/auth/oidc/result",
		"/api/v1/auth/oidc/backchannel-logout",
	} {
		r.Any(path, func(c *gin.Context) { c.Status(403) })
	}
	r.POST("/api/v1/knowledge-chat", func(c *gin.Context) { c.Status(200) })
	const secret = "synthetic-private-APM-value"
	for _, path := range []string{
		"/api/v1/auth/oidc/callback",
		"/api/v1/auth/oidc/result",
		"/api/v1/auth/oidc/backchannel-logout",
		"/api/v1/auth/oidc/unknown",
	} {
		req := httptest.NewRequest("POST", path+"?code="+secret+"&state="+secret, strings.NewReader(secret))
		req.Header.Set("Authorization", secret)
		req.Header.Set("Cookie", secret)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(exp.GetSpans()) != 0 {
		t.Fatal("authentication request exported a trace")
	}
	r.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest("POST", "/api/v1/knowledge-chat?client_secret="+secret, nil),
	)
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatal("ordinary business tracing missing")
	}
	encoded, err := json.Marshal(spans)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "http.query") {
		t.Fatal("query credentials exported to APM")
	}
}
