package middleware

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// KBDeletionMetadata must precede the existing ownership and write guards on
// DELETE /knowledge-bases/:id. It changes neither guard's authorization rules.
func KBDeletionMetadata() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodDelete && c.Param("id") != "" {
			c.Request = c.Request.WithContext(types.WithKBDeletionMetadata(c.Request.Context(), c.Param("id")))
		}
		c.Next()
	}
}
