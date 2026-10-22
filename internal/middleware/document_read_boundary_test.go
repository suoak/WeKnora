package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDocumentM3BoundaryNoHeaderBypassAndWithdrawal(t *testing.T) {
	for _, scenario := range []string{"allow", "withdraw", "no-inner", "stream", "overflow", "uncertified", "header"} {
		t.Run(scenario, func(t *testing.T) {
			active := true
			boundary := NewDocumentReadBoundary(true, map[string]DocumentReadRoute{"GET /api/v1/knowledge/:id": {
				Authorize: func(c *gin.Context) (func() error, error) {
					// A server-authenticated context flag stands for the Auth layer;
					// X-From-MCP/User-Agent/X-Tenant-ID confer no authority.
					if authenticated, _ := c.Get("synthetic.auth"); authenticated != true {
						return nil, errors.New("unauthenticated")
					}
					return func() error {
						if !active {
							return errors.New("revoked")
						}
						return nil
					}, nil
				}, ValidateResponse: func(body []byte) error {
					if strings.Contains(string(body), "unknown-source") {
						return errors.New("HOLD")
					}
					return nil
				},
			}})
			router := gin.New()
			router.Use(boundary.Outer())
			// Simulate an anonymous route registered BEFORE Auth.
			router.GET("/files/presigned", func(c *gin.Context) { c.String(200, "SECRET_ANONYMOUS_CANARY") })
			router.Use(func(c *gin.Context) {
				if scenario != "header" {
					c.Set("synthetic.auth", true)
				}
				c.Next()
			})
			if scenario != "no-inner" {
				router.Use(boundary.Inner())
			}
			router.GET("/api/v1/knowledge/:id", func(c *gin.Context) {
				c.Header("Location", "https://storage.invalid/old-link")
				if scenario == "stream" {
					c.Writer.Flush()
				}
				if scenario == "overflow" {
					_, _ = c.Writer.Write([]byte(strings.Repeat("x", maxDocumentResponseBytes+1)))
					return
				}
				c.JSON(200, gin.H{"content": "synthetic authorized"})
				if scenario == "withdraw" {
					active = false
				}
			})
			path := "/api/v1/knowledge/doc"
			if scenario == "uncertified" {
				path = "/files/presigned"
			}
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("X-From-MCP", "false")
			request.Header.Set("User-Agent", "trusted-web")
			request.Header.Set("X-Tenant-ID", "99")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if scenario == "allow" {
				require.Equal(t, http.StatusOK, response.Code)
				require.Contains(t, response.Body.String(), "synthetic authorized")
				require.Empty(t, response.Header().Get("Location"))
				require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
			} else {
				require.Equal(t, http.StatusForbidden, response.Code)
				require.NotContains(t, response.Body.String(), "synthetic authorized")
				require.NotContains(t, response.Body.String(), "CANARY")
			}
		})
	}
}
