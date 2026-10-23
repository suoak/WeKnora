package router

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type legacyReadScopeGate interface{ CheckLegacyReadScope(context.Context) error }
type legacyScopedResponse struct {
	gin.ResponseWriter
	ctx            context.Context
	requestContext func() context.Context
	gate           legacyReadScopeGate
	denied         bool
}

func (w *legacyScopedResponse) Write(body []byte) (int, error) {
	ctx := w.ctx
	if w.requestContext != nil {
		ctx = w.requestContext()
	}
	if types.LegacyReadPartial(ctx) {
		w.Header().Set("X-KnowHub-Knowledge-Scope", "partial")
		w.Header().Set("Warning", `299 KnowHub "Unavailable sources excluded; results do not cover all knowledge"`)
	}
	if w.denied {
		return 0, types.ErrLegacySourceRestricted
	}
	if err := w.gate.CheckLegacyReadScope(ctx); err != nil {
		w.denied = true
		w.Header().Set("Cache-Control", "private, no-store")
		if !w.Written() {
			w.Header().Del("Content-Length")
			w.Header().Del("Content-Encoding")
			w.Header().Del("Content-Range")
			w.Header().Set("Content-Type", "application/json")
			w.ResponseWriter.WriteHeader(http.StatusForbidden)
			_, _ = w.ResponseWriter.Write([]byte(`{"error":"source_access_requires_verification"}`))
		}
		return 0, err
	}
	return w.ResponseWriter.Write(body)
}
func (w *legacyScopedResponse) WriteString(body string) (int, error) { return w.Write([]byte(body)) }
func legacyReadScopeMiddleware(service any) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := types.WithLegacyReadScope(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		if gate, ok := service.(legacyReadScopeGate); ok {
			c.Writer = &legacyScopedResponse{ResponseWriter: c.Writer, ctx: ctx, gate: gate, requestContext: func() context.Context { return c.Request.Context() }}
		}
		c.Next()
	}
}
