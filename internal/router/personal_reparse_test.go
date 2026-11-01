package router

import (
	"context"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type personalReparseRouteService struct {
	interfaces.KnowledgeService
	rejection error
}

func (s personalReparseRouteService) ValidateKnowledgeReparse(context.Context, string) error {
	return s.rejection
}

func TestPersonalReparseAdmissionBeforeDestructiveHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, rejected := range []bool{false, true} {
		service := personalReparseRouteService{}
		if rejected {
			service.rejection = apperrors.NewConflictError("use controlled source recovery")
		}
		guards := &rbacGuards{knowledgeService: service}
		router := gin.New()
		router.Use(middleware.ErrorHandler())
		called := false
		router.POST("/knowledge/:id/reparse", guards.PersonalReparseAdmission("id"), func(c *gin.Context) {
			called = true
			c.JSON(200, gin.H{"success": true})
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/knowledge/doc/reparse", nil))
		require.Equal(t, !rejected, called)
		if rejected {
			require.Equal(t, 409, response.Code)
			require.Contains(t, response.Body.String(), "use controlled source recovery")
		} else {
			require.Equal(t, 200, response.Code)
		}
	}
}
