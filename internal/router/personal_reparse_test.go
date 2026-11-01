package router

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type personalReparseRouteService struct {
	interfaces.KnowledgeService
	rejection error
}

type exactRetryRouteService struct {
	interfaces.KnowledgeService
	handled   bool
	rejection error
}

func (s exactRetryRouteService) TryPersonalReparse(
	context.Context, string,
) (bool, *types.PersonalExactRetryAccepted, error) {
	accepted := &types.PersonalExactRetryAccepted{
		KnowledgeID: "doc", OperationID: 12, State: "pending", Recovery: "personal_exact_retry",
	}
	return s.handled, accepted, s.rejection
}

func TestPersonalExactRetryRouteNeverCallsDestructiveHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{200, 202, 403, 409, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := exactRetryRouteService{handled: status != 200}
			switch status {
			case 403:
				s.rejection = apperrors.NewForbiddenError("请重新授权飞书")
			case 409:
				s.rejection = apperrors.NewConflictError("文件版本已变化")
			case 503:
				s.rejection = apperrors.NewServiceUnavailableError("暂时无法验证飞书文档")
			}
			guards := &rbacGuards{knowledgeService: s}
			r := gin.New()
			r.Use(middleware.ErrorHandler())
			called := false
			r.POST("/knowledge/:id/reparse", guards.PersonalReparseAdmission("id"), func(c *gin.Context) {
				called = true
				c.JSON(200, gin.H{"success": true})
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/knowledge/doc/reparse", nil))
			require.Equal(t, status, w.Code)
			require.Equal(t, status == 200, called)
			if status == 202 {
				require.Contains(t, w.Body.String(), "recovery_operation_id")
				require.NotContains(t, w.Body.String(), "nonce")
			}
		})
	}
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
