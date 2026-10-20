package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Recovery is a middleware that recovers from panics
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				if utils.IsAuthenticationPath(c.Request.URL.Path) {
					// Panic values and stacks can embed credentials supplied by a handler.
					logger.Error(c.Request.Context(), "authentication handler panic")
					c.AbortWithStatusJSON(500, gin.H{"error": "Internal Server Error"})
					return
				}
				// Get request ID from context
				ctx := c.Request.Context()
				requestID, _ := c.Get("RequestID")

				// Print stacktrace
				stacktrace := debug.Stack()
				// Log error with structured logger
				logger.ErrorWithFields(ctx, fmt.Errorf("panic: %v", err), logrus.Fields{
					"request_id": requestID,
					"stacktrace": string(stacktrace),
				})

				// 返回500错误
				c.AbortWithStatusJSON(500, gin.H{
					"error":   "Internal Server Error",
					"message": fmt.Sprintf("%v", err),
				})
			}
		}()

		c.Next()
	}
}
