package middleware

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/access/httpboundary"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// DocumentCoreReadBoundary reuses the existing bounded JSON response buffer.
// The certified set contains only actual Knowledge/Chunk read routes. Other
// content operations, including mutations, remain HOLD; unrelated administrative
// routes retain their existing RBAC. This mode is an isolated pilot only.
type DocumentCoreReadBoundary struct {
	enabled bool
	base    *httpboundary.DocumentReadBoundary
}

func NewDocumentCoreReadBoundary(knowledge interfaces.KnowledgeService, chunks interfaces.ChunkService) *DocumentCoreReadBoundary {
	marker, ok := knowledge.(interface{ DocumentACLReadEnabled() bool })
	enabled := ok && marker.DocumentACLReadEnabled()
	routes := map[string]httpboundary.DocumentReadRoute{}
	for _, entry := range []struct{ key, kind string }{
		{"GET /api/v1/knowledge/:id", "knowledge"}, {"GET /api/v1/knowledge/batch", "batch"},
		{"GET /api/v1/knowledge-bases/:id/knowledge", "kb"}, {"GET /api/v1/chunks/:knowledge_id", "chunks"}, {"GET /api/v1/chunks/by-id/:id", "chunk"},
	} {
		kind := entry.kind
		routes[entry.key] = httpboundary.DocumentReadRoute{Authorize: func(c *gin.Context) (func() error, error) {
			ctx, check, err := access.WithDocumentReadOutput(c.Request.Context())
			if err != nil {
				return nil, err
			}
			c.Request = c.Request.WithContext(ctx)
			// Seed a live opaque proof before the existing boundary's admission check.
			// Actual handlers/services read again and append their final output proofs.
			switch kind {
			case "knowledge":
				_, err = knowledge.GetKnowledgeByIDOnly(ctx, c.Param("id"))
			case "chunks":
				_, err = knowledge.GetKnowledgeByIDOnly(ctx, c.Param("knowledge_id"))
			case "chunk":
				_, err = chunks.GetChunkByIDOnly(ctx, c.Param("id"))
			case "kb":
				svc, ok := knowledge.(interface {
					DocumentKBReadContext(context.Context, string) (context.Context, error)
				})
				if !ok {
					return nil, access.ErrForbidden
				}
				ctx, err = svc.DocumentKBReadContext(ctx, c.Param("id"))
				if err == nil {
					c.Request = c.Request.WithContext(ctx)
				}
			case "batch":
				var input struct {
					IDs []string `form:"ids"`
				}
				if err = c.ShouldBindQuery(&input); err == nil {
					_, err = knowledge.GetKnowledgeBatch(ctx, types.CallerFromContext(ctx).TenantID, input.IDs)
				}
			}
			if err != nil {
				return nil, err
			}
			return check, nil
		}, ValidateResponse: func(body []byte) error {
			if !json.Valid(body) {
				return access.ErrForbidden
			}
			return nil
		}}
	}
	return &DocumentCoreReadBoundary{enabled: enabled, base: httpboundary.NewDocumentReadBoundary(enabled, routes)}
}

func protectedDocumentHTTP(c *gin.Context) bool {
	path := c.FullPath()
	// Exact route identity comes from Gin, not URL/Header assertions. Unknown
	// content and model routes are held for every operation until certified.
	if path == "/api/v1/mcp/usage" && c.Request.Method == "POST" {
		return false
	}
	for _, prefix := range []string{"/api/v1/knowledge", "/api/v1/chunks", "/api/v1/files", "/files", "/r/", "/api/v1/messages", "/api/v1/sessions", "/api/v1/artifacts", "/api/v1/embed", "/api/v1/im/callback/", "/api/v1/agent-chat", "/api/v1/mcp", "/mcp", "/sse"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
func (b *DocumentCoreReadBoundary) Outer() gin.HandlerFunc {
	return func(c *gin.Context) {
		if b != nil && b.enabled && protectedDocumentHTTP(c) {
			b.base.Outer()(c)
			return
		}
		c.Next()
	}
}
func (b *DocumentCoreReadBoundary) Inner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if b != nil && b.enabled && protectedDocumentHTTP(c) {
			b.base.Inner()(c)
			return
		}
		c.Next()
	}
}
