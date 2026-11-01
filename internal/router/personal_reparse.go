package router

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"

	"github.com/gin-gonic/gin"
)

func (g *rbacGuards) PersonalReparseAdmission(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service, ok := g.knowledgeService.(interface {
			TryPersonalReparse(context.Context, string) (bool, *types.PersonalExactRetryAccepted, error)
		}); ok {
			handled, accepted, err := service.TryPersonalReparse(c.Request.Context(), c.Param(param))
			if err != nil {
				_ = c.Error(err)
				c.Abort()
				return
			}
			if handled {
				c.JSON(http.StatusAccepted, gin.H{"success": true, "data": accepted})
				c.Abort()
				return
			}
			c.Next()
			return
		}
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
