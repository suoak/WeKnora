package middleware

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// PersonalMCPAuth reuses normal Auth and user_mcp ownership/membership checks.
// It only translates Bearer credentials and chooses a persisted default scope;
// a caller-specified workspace is still checked by the existing Auth layer.
func PersonalMCPAuth(tenants interfaces.TenantService, users interfaces.UserService, members interfaces.TenantMemberService, keys interfaces.TenantAPIKeyService, cfg *config.Config) gin.HandlerFunc {
	auth := Auth(tenants, users, members, keys, cfg)
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok || keys == nil || users == nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		key, err := keys.AuthenticateAPIKey(c.Request.Context(), token)
		if err != nil || key == nil || !key.IsUserMCP() || key.FullAccess || key.OwnerUserID == nil || !(types.TenantAPIKeyScope{Capabilities: key.Capabilities}).HasCapability(types.APIKeyCapabilityRetrieve) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		owner, err := users.GetUserByID(c.Request.Context(), *key.OwnerUserID)
		if err != nil || owner == nil || !owner.IsActive {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if c.GetHeader("X-Tenant-ID") == "" {
			var tenant uint64
			if key.PersonalScopeMode == "dynamic_all" {
				if members == nil {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
				liveMembers, err := members.ListByUser(c.Request.Context(), owner.ID)
				if err != nil {
					c.AbortWithStatus(http.StatusServiceUnavailable)
					return
				}
				for _, member := range liveMembers {
					if member != nil && member.Status == types.TenantMemberStatusActive && !member.DeletedAt.Valid && member.Role.IsValid() && (tenant == 0 || member.TenantID == owner.TenantID || member.TenantID < tenant && tenant != owner.TenantID) {
						tenant = member.TenantID
					}
				}
			}

			if key.PersonalScopeMode != "dynamic_all" {
				for _, scope := range key.TenantScopes {
					if scope.TenantID == owner.TenantID {
						tenant = scope.TenantID
						break
					}
					if tenant == 0 || scope.TenantID < tenant {
						tenant = scope.TenantID
					}
				}
			}
			if tenant == 0 {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Request.Header.Set("X-Tenant-ID", strconv.FormatUint(tenant, 10))
		}
		c.Request.Header.Del("Authorization")
		c.Request.Header.Set("X-API-Key", token)
		auth(c)
	}
}
