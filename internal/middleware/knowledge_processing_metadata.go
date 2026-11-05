package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// KnowledgeProcessingMetadata precedes the normal Viewer and KB guards. Only
// stages/spans GET routes may request this projection; bodies keep their gates.
func KnowledgeProcessingMetadata(lookup KnowledgeLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, supported := lookup.(interface {
			GetKnowledgeProcessingMetadata(context.Context, string) (*types.Knowledge, error)
		})
		path := c.FullPath()
		if supported && c.Request.Method == http.MethodGet && c.Param("id") != "" &&
			(strings.HasSuffix(path, "/:id/spans") || strings.HasSuffix(path, "/:id/stages")) {
			ctx := types.WithKnowledgeProcessingMetadata(c.Request.Context(), c.Param("id"))
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
