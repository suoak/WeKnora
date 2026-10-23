package router

import (
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

func RegisterPersonalMCPRoutes(r *gin.Engine, srv *mcpserver.PersonalServer, tenants interfaces.TenantService, users interfaces.UserService, members interfaces.TenantMemberService, keys interfaces.TenantAPIKeyService, cfg *config.Config) {
	if srv == nil {
		return
	}
	auth := middleware.PersonalMCPAuth(tenants, users, members, keys, cfg)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		if !srv.Enabled() {
			r.Handle(method, "/api/v1/mcp/personal", gin.WrapH(srv.Handler()))
			continue
		}
		r.Handle(method, "/api/v1/mcp/personal", personalPerformanceTrace, auth, gin.WrapH(srv.Handler()))
	}
}

// personalPerformanceTrace emits one bounded metrics record, without RPC inputs.
func personalPerformanceTrace(c *gin.Context) {
	ctx, trace := perftrace.New(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	if trace != nil {
		c.Header("X-KnowHub-Perf-ID", trace.ID())
	}
	defer func() {
		if trace != nil {
			raw, err := json.Marshal(trace.Summary())
			if err == nil {
				logger.Infof(ctx, "[PersonalMCPPerf] %s", string(raw))
			}
		}
	}()
	c.Next()
}
