package router

import (
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/middleware"
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
		r.Handle(method, "/api/v1/mcp/personal", auth, gin.WrapH(srv.Handler()))
	}
}
