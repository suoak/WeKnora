package router

import (
	"context"
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// These guards are registered only for document read routes. They never mint a
// KBAccess/write grant; all mutations retain the existing ownership/KB guards.
func (g *rbacGuards) DocumentReadFromKnowledge(param string) gin.HandlerFunc {
	legacy := g.KBAccessReadFromKnowledgeIDParam(param)
	return func(c *gin.Context) {
		if exact, ok := g.knowledgeService.(interface {
			ExactPublishedReadContext(context.Context, string) (context.Context, bool, error)
		}); ok {
			ctx, handled, err := exact.ExactPublishedReadContext(c.Request.Context(), c.Param(param))
			if err != nil {
				failure := apperrors.NewInternalServerError("Document publication verification failed")
				var typed *apperrors.AppError
				if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, access.ErrNotFound) {
					failure = apperrors.NewNotFoundError("Document unavailable")
				} else if errors.As(err, &typed) {
					switch typed.HTTPCode {
					case http.StatusForbidden:
						failure = apperrors.NewForbiddenError("Document read denied")
					case http.StatusUnauthorized:
						failure = apperrors.NewUnauthorizedError("Authentication required")
					case http.StatusNotFound:
						failure = apperrors.NewNotFoundError("Document unavailable")
					}
				}
				if errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrUnauthorized) ||
					errors.Is(err, types.ErrExactDocumentScope) || errors.Is(err, types.ErrLegacySourceRestricted) {
					failure = apperrors.NewForbiddenError("Document read denied")
				}
				if errors.Is(err, service.ErrPersonalPublishedSnapshotUnverified) {
					message := "文档尚未发布或本地发布副本未通过完整性核验，请联系管理员核查。"
					failure = apperrors.NewForbiddenError(message).WithDetails(map[string]any{
						"reason":    "personal_published_snapshot_unverified",
						"action":    "inspect_published_snapshot",
						"retryable": false,
					})
				} else if errors.Is(err, core.ErrTransient) || errors.Is(err, core.ErrRateLimited) ||
					errors.Is(err, context.DeadlineExceeded) {
					message := "本地发布副本核验暂时不可用，请稍后重试；文档内容已保留。"
					failure = apperrors.NewServiceUnavailableError(message)
					failure = failure.WithDetails(map[string]any{
						"reason":    "personal_published_verification_temporarily_unavailable",
						"retryable": true,
					})
				}
				_ = c.Error(failure)
				c.Abort()
				return
			}
			if handled {
				// The issuer independently resolves the canonical viewer grant;
				// this capability cannot read another document or a whole KB.
				c.Request = c.Request.WithContext(ctx)
				c.Next()
				return
			}
		}
		svc, ok := g.knowledgeService.(service.KnowledgeDocumentReadService)
		if !ok || !svc.DocumentACLReadEnabled() {
			legacy(c)
			return
		}
		if !access.HasDocumentReadOutput(c.Request.Context()) {
			_ = c.Error(apperrors.NewForbiddenError("Document output boundary required"))
			c.Abort()
			return
		}
		row, err := g.knowledgeService.GetKnowledgeByIDOnly(c.Request.Context(), c.Param(param))
		if err != nil || row == nil {
			_ = c.Error(apperrors.NewForbiddenError("Document read denied"))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(types.WithExecutionTenant(c.Request.Context(), row.TenantID))
		c.Next()
	}
}

func (g *rbacGuards) DocumentReadFromChunk(param string) gin.HandlerFunc {
	legacy := g.KBAccessReadFromChunkIDParam(param)
	return func(c *gin.Context) {
		svc, ok := g.knowledgeService.(service.KnowledgeDocumentReadService)
		if !ok || !svc.DocumentACLReadEnabled() {
			legacy(c)
			return
		}
		if !access.HasDocumentReadOutput(c.Request.Context()) {
			_ = c.Error(apperrors.NewForbiddenError("Document output boundary required"))
			c.Abort()
			return
		}
		row, err := g.chunkService.GetChunkByIDOnly(c.Request.Context(), c.Param(param))
		if err != nil || row == nil {
			_ = c.Error(apperrors.NewForbiddenError("Document read denied"))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(types.WithExecutionTenant(c.Request.Context(), row.TenantID))
		c.Next()
	}
}

func (g *rbacGuards) DocumentReadFromKB(param string) gin.HandlerFunc {
	legacy := g.KBAccessRead(param)
	return func(c *gin.Context) {
		svc, ok := g.knowledgeService.(service.KnowledgeDocumentReadService)
		if !ok || !svc.DocumentACLReadEnabled() {
			legacy(c)
			return
		}
		if !access.HasDocumentReadOutput(c.Request.Context()) {
			_ = c.Error(apperrors.NewForbiddenError("Document output boundary required"))
			c.Abort()
			return
		}
		ctx, err := svc.DocumentKBReadContext(c.Request.Context(), c.Param(param))
		if err != nil {
			_ = c.Error(apperrors.NewForbiddenError("Document list denied"))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
