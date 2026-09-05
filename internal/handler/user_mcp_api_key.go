package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type userMCPKBRequest struct {
	KnowledgeBaseID string                   `json:"knowledge_base_id"`
	SourceType      types.APIKeyKBSourceType `json:"source_type"`
	KBShareID       *string                  `json:"kb_share_id"`
}
type userMCPTenantScopeRequest struct {
	TenantID       uint64                  `json:"tenant_id"`
	KBScopeMode    types.APIKeyKBScopeMode `json:"kb_scope_mode"`
	KnowledgeBases []userMCPKBRequest      `json:"knowledge_bases"`
}
type userMCPKeyRequest struct {
	Name         string                      `json:"name"`
	ExpiresAt    *int64                      `json:"expires_at_unix"`
	NeverExpires bool                        `json:"never_expires"`
	TenantScopes []userMCPTenantScopeRequest `json:"tenant_scopes"`
}

func (h *TenantHandler) userMCPKeyService() (interfaces.UserMCPAPIKeyService, bool) {
	s, ok := h.apiKeyService.(interfaces.UserMCPAPIKeyService)
	return s, ok
}

func (h *TenantHandler) validateUserMCPScopes(c *gin.Context, userID string, input []userMCPTenantScopeRequest) ([]types.APIKeyTenantScope, error) {
	if len(input) == 0 {
		return nil, errors.NewValidationError("at least one workspace is required")
	}
	seen := map[uint64]bool{}
	out := make([]types.APIKeyTenantScope, 0, len(input))
	for _, in := range input {
		if in.TenantID == 0 || seen[in.TenantID] {
			return nil, errors.NewValidationError("workspace scopes must be unique")
		}
		seen[in.TenantID] = true
		m, err := h.memberService.GetMembership(c.Request.Context(), userID, in.TenantID)
		if err != nil || m == nil || m.Status != types.TenantMemberStatusActive {
			return nil, errors.NewForbiddenError("workspace is not an active membership")
		}
		if in.KBScopeMode != types.APIKeyKBScopeAll && in.KBScopeMode != types.APIKeyKBScopeSelected {
			return nil, errors.NewValidationError("kb_scope_mode must be all or selected")
		}
		if in.KBScopeMode == types.APIKeyKBScopeSelected && len(in.KnowledgeBases) == 0 {
			return nil, errors.NewValidationError("selected scope requires knowledge bases")
		}
		if in.KBScopeMode == types.APIKeyKBScopeAll && len(in.KnowledgeBases) != 0 {
			return nil, errors.NewValidationError("all scope must not include knowledge bases")
		}
		s := types.APIKeyTenantScope{TenantID: in.TenantID, KBScopeMode: in.KBScopeMode}
		seenKBs := map[string]bool{}
		for _, requested := range in.KnowledgeBases {
			identity := string(requested.SourceType) + ":" + strings.TrimSpace(requested.KnowledgeBaseID)
			if seenKBs[identity] {
				return nil, errors.NewValidationError("knowledge base scopes must be unique")
			}
			seenKBs[identity] = true
			kb, err := h.kbService.GetKnowledgeBaseByIDOnly(c.Request.Context(), strings.TrimSpace(requested.KnowledgeBaseID))
			if err != nil || kb == nil {
				return nil, errors.NewValidationError("knowledge base not found")
			}
			row := types.APIKeyKnowledgeBaseScope{TenantID: in.TenantID, KnowledgeBaseID: kb.ID, SourceType: requested.SourceType, KBShareID: requested.KBShareID}
			if requested.SourceType == types.APIKeyKBSourceOwned {
				if kb.TenantID != in.TenantID || requested.KBShareID != nil {
					return nil, errors.NewValidationError("invalid owned knowledge base scope")
				}
			} else if requested.SourceType == types.APIKeyKBSourceShared {
				if requested.KBShareID == nil || h.kbShareService == nil {
					return nil, errors.NewValidationError("shared knowledge base requires share id")
				}
				share, e := h.kbShareService.GetShare(c.Request.Context(), *requested.KBShareID)
				if e != nil || share == nil || share.KnowledgeBaseID != kb.ID {
					return nil, errors.NewValidationError("invalid knowledge base share")
				}
				available, e := h.kbShareService.ListSharedKnowledgeBases(c.Request.Context(), in.TenantID, m.Role)
				exactShareAllowed := false
				for _, item := range available {
					if item != nil && item.ShareID == *requested.KBShareID && item.KnowledgeBase != nil && item.KnowledgeBase.ID == kb.ID {
						exactShareAllowed = true
						break
					}
				}
				if e != nil || !exactShareAllowed {
					return nil, errors.NewForbiddenError("shared knowledge base is not accessible")
				}
			} else {
				return nil, errors.NewValidationError("source_type must be owned or shared")
			}
			s.KnowledgeBases = append(s.KnowledgeBases, row)
		}
		out = append(out, s)
	}
	return out, nil
}

func userMCPExpiry(req userMCPKeyRequest) (*time.Time, error) {
	if req.NeverExpires {
		return nil, nil
	}
	if req.ExpiresAt == nil {
		v := time.Now().UTC().Add(90 * 24 * time.Hour)
		return &v, nil
	}
	v := time.Unix(*req.ExpiresAt, 0).UTC()
	if !v.After(time.Now().UTC()) {
		return nil, errors.NewValidationError("expires_at_unix must be in the future")
	}
	return &v, nil
}

func mcpPublicURL() string {
	value := strings.TrimSpace(os.Getenv("MCP_PUBLIC_URL"))
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	if strings.EqualFold(os.Getenv("GIN_MODE"), "release") && parsed.Scheme != "https" {
		return ""
	}
	return strings.TrimRight(value, "/")
}
func userMCPResponse(k *types.TenantAPIKey) gin.H {
	return gin.H{"id": k.ID, "scope_type": k.ScopeType, "name": k.Name, "api_key": maskManagedAPIKey(k.APIKey), "tenant_scopes": k.TenantScopes, "last_used_at": k.LastUsedAt, "expires_at": k.ExpiresAt, "created_at": k.CreatedAt}
}

func (h *TenantHandler) auditUserMCPKey(c *gin.Context, action types.AuditAction, userID string, keyID uint64, name string, scopes []types.APIKeyTenantScope) {
	if h.auditService == nil {
		return
	}
	details, _ := json.Marshal(gin.H{"key_name": name, "workspace_count": len(scopes)})
	for _, scope := range scopes {
		_ = h.auditService.Log(c.Request.Context(), &types.AuditLog{
			TenantID:      scope.TenantID,
			ActorUserID:   userID,
			Action:        action,
			ScopeType:     "api_key",
			ScopeID:       strconv.FormatUint(keyID, 10),
			TargetType:    "user_mcp_key",
			TargetID:      strconv.FormatUint(keyID, 10),
			RequestPath:   c.Request.URL.Path,
			RequestMethod: c.Request.Method,
			Outcome:       types.AuditOutcomeSuccess,
			Details:       types.JSON(details),
		})
	}
}

func findUserMCPKey(rows []*types.TenantAPIKey, id uint64) *types.TenantAPIKey {
	for _, row := range rows {
		if row != nil && row.ID == id {
			return row
		}
	}
	return nil
}

func (h *TenantHandler) ListUserMCPAPIKeys(c *gin.Context) {
	uid := c.GetString(types.UserIDContextKey.String())
	svc, ok := h.userMCPKeyService()
	if !ok {
		c.Error(errors.NewInternalServerError("MCP API key service unavailable"))
		return
	}
	rows, e := svc.ListUserMCPAPIKeys(c.Request.Context(), uid)
	if e != nil {
		c.Error(errors.NewInternalServerError(e.Error()))
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, k := range rows {
		data = append(data, userMCPResponse(k))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data, "mcp_public_url": mcpPublicURL()})
}
func (h *TenantHandler) CreateUserMCPAPIKey(c *gin.Context) {
	var req userMCPKeyRequest
	if e := c.ShouldBindJSON(&req); e != nil {
		c.Error(errors.NewValidationError("invalid request").WithDetails(e.Error()))
		return
	}
	uid := c.GetString(types.UserIDContextKey.String())
	scopes, e := h.validateUserMCPScopes(c, uid, req.TenantScopes)
	if e != nil {
		c.Error(e)
		return
	}
	exp, e := userMCPExpiry(req)
	if e != nil {
		c.Error(e)
		return
	}
	result, e := h.apiKeyService.CreateAPIKey(c.Request.Context(), interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: uid, Name: req.Name, Capabilities: []string{"retrieve", "chat", "read_agents"}, ExpiresAt: exp, TenantScopes: scopes})
	if e != nil {
		c.Error(errors.NewValidationError(e.Error()))
		return
	}
	resp := userMCPResponse(result.APIKey)
	resp["token"] = result.Token
	resp["mcp_public_url"] = mcpPublicURL()
	h.auditUserMCPKey(c, types.AuditActionUserMCPKeyCreated, uid, result.APIKey.ID, result.APIKey.Name, result.APIKey.TenantScopes)
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": resp})
}
func (h *TenantHandler) UpdateUserMCPAPIKey(c *gin.Context) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil {
		c.Error(errors.NewValidationError("invalid key id"))
		return
	}
	var req userMCPKeyRequest
	if e = c.ShouldBindJSON(&req); e != nil {
		c.Error(errors.NewValidationError("invalid request"))
		return
	}
	uid := c.GetString(types.UserIDContextKey.String())
	svc, ok := h.userMCPKeyService()
	if !ok {
		c.Error(errors.NewInternalServerError("MCP API key service unavailable"))
		return
	}
	oldRows, _ := svc.ListUserMCPAPIKeys(c.Request.Context(), uid)
	oldKey := findUserMCPKey(oldRows, id)
	scopes, e := h.validateUserMCPScopes(c, uid, req.TenantScopes)
	if e != nil {
		c.Error(e)
		return
	}
	exp, e := userMCPExpiry(req)
	if e != nil {
		c.Error(e)
		return
	}
	row, e := svc.ReplaceUserMCPAPIKey(c.Request.Context(), uid, &types.TenantAPIKey{ID: id, Name: strings.TrimSpace(req.Name), ExpiresAt: exp, TenantScopes: scopes})
	if e != nil {
		c.Error(errors.NewNotFoundError("MCP API key not found"))
		return
	}
	auditScopes := append([]types.APIKeyTenantScope{}, scopes...)
	if oldKey != nil {
		seen := map[uint64]bool{}
		for _, scope := range auditScopes {
			seen[scope.TenantID] = true
		}
		for _, scope := range oldKey.TenantScopes {
			if !seen[scope.TenantID] {
				auditScopes = append(auditScopes, scope)
			}
		}
	}
	h.auditUserMCPKey(c, types.AuditActionUserMCPKeyUpdated, uid, row.ID, row.Name, auditScopes)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": userMCPResponse(row)})
}
func (h *TenantHandler) RevokeUserMCPAPIKey(c *gin.Context) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil {
		c.Error(errors.NewValidationError("invalid key id"))
		return
	}
	uid := c.GetString(types.UserIDContextKey.String())
	svc, ok := h.userMCPKeyService()
	if !ok {
		c.Error(errors.NewInternalServerError("MCP API key service unavailable"))
		return
	}
	rows, _ := svc.ListUserMCPAPIKeys(c.Request.Context(), uid)
	key := findUserMCPKey(rows, id)
	if e = svc.RevokeUserMCPAPIKey(c.Request.Context(), uid, id); e != nil {
		c.Error(errors.NewNotFoundError("MCP API key not found"))
		return
	}
	if key != nil {
		h.auditUserMCPKey(c, types.AuditActionUserMCPKeyRevoked, uid, key.ID, key.Name, key.TenantScopes)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *TenantHandler) UserMCPAPIKeyScopeOptions(c *gin.Context) {
	uid := c.GetString(types.UserIDContextKey.String())
	members, e := h.memberService.ListByUser(c.Request.Context(), uid)
	if e != nil {
		c.Error(errors.NewInternalServerError(e.Error()))
		return
	}
	items := make([]gin.H, 0, len(members))
	for _, m := range members {
		if m == nil || m.Status != types.TenantMemberStatusActive {
			continue
		}
		t, e := h.service.GetTenantByID(c.Request.Context(), m.TenantID)
		if e != nil || t == nil || !strings.EqualFold(strings.TrimSpace(t.Status), "active") {
			continue
		}
		owned, ownedErr := h.kbService.ListKnowledgeBasesByTenantID(c.Request.Context(), m.TenantID)
		if ownedErr != nil {
			logger.ErrorWithFields(c.Request.Context(), ownedErr, map[string]interface{}{
				"tenant_id": m.TenantID,
				"operation": "list_user_mcp_scope_owned_knowledge_bases",
			})
			c.Error(errors.NewInternalServerError("failed to load workspace knowledge bases"))
			return
		}
		shared := []*types.SharedKnowledgeBaseInfo{}
		if h.kbShareService != nil {
			shared, e = h.kbShareService.ListSharedKnowledgeBases(c.Request.Context(), m.TenantID, m.Role)
			if e != nil {
				logger.ErrorWithFields(c.Request.Context(), e, map[string]interface{}{
					"tenant_id": m.TenantID,
					"operation": "list_user_mcp_scope_shared_knowledge_bases",
				})
				c.Error(errors.NewInternalServerError("failed to load shared knowledge bases"))
				return
			}
		}
		sharedSpaces := map[string]gin.H{}
		for _, item := range shared {
			if item == nil {
				continue
			}
			group, ok := sharedSpaces[item.OrganizationID]
			if !ok {
				group = gin.H{"organization_id": item.OrganizationID, "organization_name": item.OrgName, "knowledge_bases": []*types.SharedKnowledgeBaseInfo{}}
			}
			group["knowledge_bases"] = append(group["knowledge_bases"].([]*types.SharedKnowledgeBaseInfo), item)
			sharedSpaces[item.OrganizationID] = group
		}
		items = append(items, gin.H{"tenant_id": m.TenantID, "tenant_name": t.Name, "role": m.Role, "owned_knowledge_bases": owned, "shared_knowledge_bases": shared, "shared_spaces": sharedSpaces})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items, "mcp_public_url": mcpPublicURL()})
}
