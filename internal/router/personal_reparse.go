package router

import (
	"context"

	"github.com/gin-gonic/gin"
)

func (g *rbacGuards) PersonalReparseAdmission(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service, ok := g.knowledgeService.(interface {
			ValidateKnowledgeReparse(context.Context, string) error
		}); ok {
			if err := service.ValidateKnowledgeReparse(c.Request.Context(), c.Param(param)); err != nil {
				_ = c.Error(err)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
