package middleware

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// KnowledgeDeletionMetadata runs before the ordinary ownership/write guards.
// Only a metadata projection is requested, even for an unpublished document.
func KnowledgeDeletionMetadata(lookup KnowledgeLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		reader, ok := lookup.(interface {
			GetKnowledgeDeletionMetadata(context.Context, string) (*types.Knowledge, error)
		})
		id := c.Param("id")
		if !ok || c.Request.Method != http.MethodDelete || id == "" {
			c.Next()
			return
		}
		ctx := types.WithKnowledgeDeletionMetadata(c.Request.Context(), id)
		row, err := reader.GetKnowledgeDeletionMetadata(ctx, id)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		ctx = types.WithKBDeletionMetadata(ctx, row.KnowledgeBaseID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
