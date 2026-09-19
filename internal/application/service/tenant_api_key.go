package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// apiKeyLastUsedMinInterval bounds how often we persist last_used_at per key.
// The UI only needs minute-level freshness; throttling avoids a DB write on
// every authenticated request under high QPS.
const apiKeyLastUsedMinInterval = time.Minute

type tenantAPIKeyService struct {
	repo          interfaces.TenantAPIKeyRepository
	lastUsedTouch sync.Map // key ID (uint64) -> time.Time of last persisted touch
}

func NewTenantAPIKeyService(repo interfaces.TenantAPIKeyRepository) interfaces.TenantAPIKeyService {
	return &tenantAPIKeyService{repo: repo}
}

func (s *tenantAPIKeyService) CreateAPIKey(
	ctx context.Context, req interfaces.TenantAPIKeyCreateRequest,
) (*interfaces.TenantAPIKeyCreateResult, error) {
	scopeType := types.NormalizeAPIKeyScopeType(req.ScopeType)
	if scopeType == types.APIKeyScopeTenant && req.TenantID == 0 {
		return nil, errors.New("tenant_id is required")
	}
	if scopeType == types.APIKeyScopePlatform && req.FullAccess {
		return nil, errors.New("platform API keys require explicit capabilities")
	}
	if scopeType == types.APIKeyScopeUserMCP && (strings.TrimSpace(req.OwnerUserID) == "" || len(req.TenantScopes) == 0) {
		return nil, errors.New("user_mcp keys require owner and tenant scopes")
	}
	capabilities := types.NormalizeAPIKeyCapabilities(types.StringArray(req.Capabilities))
	var err error
	if scopeType == types.APIKeyScopeUserMCP {
		// User-owned MCP credentials remain bounded to the Phase 1 read-only
		// capability set. Keep this invariant below the HTTP handler so future
		// callers cannot elevate it.
		req.FullAccess = false
		req.KnowledgeBaseIDs = nil
		capabilities, err = normalizeUserMCPCapabilities(req.Capabilities, true)
		if err != nil {
			return nil, err
		}
	}
	if scopeType == types.APIKeyScopePlatform && len(capabilities) == 0 {
		return nil, errors.New("platform API keys require at least one capability")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	clientType, err := normalizeMCPClientTypeStrict(req.ClientType)
	if err != nil {
		return nil, err
	}
	token, err := generateTenantAPIKeyToken()
	if err != nil {
		return nil, err
	}
	expiresAt := req.ExpiresAt
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
	}
	var tenantID *uint64
	if scopeType == types.APIKeyScopeTenant {
		tenantID = &req.TenantID
	}
	var ownerUserID *string
	if scopeType == types.APIKeyScopeUserMCP {
		v := strings.TrimSpace(req.OwnerUserID)
		ownerUserID = &v
	}
	key := &types.TenantAPIKey{
		TenantID:         tenantID,
		ScopeType:        scopeType,
		Name:             name,
		ClientType:       clientType,
		KeyHash:          hashTenantAPIKey(token),
		APIKey:           token,
		FullAccess:       req.FullAccess,
		KnowledgeBaseIDs: normalizeAPIKeyIDs(req.KnowledgeBaseIDs),
		Capabilities:     capabilities,
		ExpiresAt:        expiresAt,
		OwnerUserID:      ownerUserID,
		TenantScopes:     req.TenantScopes,
	}
	if scopeType == types.APIKeyScopeUserMCP {
		key.APIKey = ""
		key.TokenHint = tenantAPIKeyTokenHint(token)
	}
	if key.FullAccess {
		key.KnowledgeBaseIDs = nil
		key.Capabilities = nil
	}
	if err := s.repo.CreateAPIKey(ctx, key); err != nil {
		return nil, err
	}
	return &interfaces.TenantAPIKeyCreateResult{APIKey: key, Token: token}, nil
}

func (s *tenantAPIKeyService) ListUserMCPAPIKeys(ctx context.Context, userID string) ([]*types.TenantAPIKey, error) {
	return s.repo.(interfaces.UserMCPAPIKeyRepository).ListUserMCPAPIKeys(ctx, userID)
}
func (s *tenantAPIKeyService) GetUserMCPAPIKey(ctx context.Context, userID string, id uint64) (*types.TenantAPIKey, error) {
	return s.repo.(interfaces.UserMCPAPIKeyRepository).GetUserMCPAPIKey(ctx, userID, id)
}
func (s *tenantAPIKeyService) GetUserMCPTenantScope(ctx context.Context, keyID, tenantID uint64) (*types.APIKeyTenantScope, error) {
	return s.repo.(interfaces.UserMCPAPIKeyRepository).GetUserMCPTenantScope(ctx, keyID, tenantID)
}
func (s *tenantAPIKeyService) ReplaceUserMCPAPIKey(ctx context.Context, userID string, key *types.TenantAPIKey) (*types.TenantAPIKey, error) {
	key.Name = strings.TrimSpace(key.Name)
	if key.Name == "" {
		return nil, errors.New("name is required")
	}
	clientType, err := normalizeMCPClientTypeStrict(key.ClientType)
	if err != nil {
		return nil, err
	}
	key.ClientType = clientType
	capabilities, err := normalizeUserMCPCapabilities([]string(key.Capabilities), false)
	if err != nil {
		return nil, err
	}
	key.Capabilities = capabilities
	if key.ExpiresAt != nil {
		utc := key.ExpiresAt.UTC()
		key.ExpiresAt = &utc
	}
	return s.repo.(interfaces.UserMCPAPIKeyRepository).ReplaceUserMCPAPIKey(ctx, userID, key)
}
func (s *tenantAPIKeyService) RotateUserMCPAPIKey(ctx context.Context, userID string, id uint64, req interfaces.UserMCPAPIKeyRotateRequest) (*interfaces.TenantAPIKeyCreateResult, error) {
	repo := s.repo.(interfaces.UserMCPAPIKeyRepository)
	current, err := repo.GetUserMCPAPIKey(ctx, userID, id)
	if err != nil || current == nil || current.RevokedAt != nil {
		return nil, apprepo.ErrTenantAPIKeyNotFound
	}
	now := time.Now().UTC()
	expiresAt := current.ExpiresAt
	if req.ExpirySpecified {
		expiresAt = req.ExpiresAt
	}
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
		if !expiresAt.After(now) {
			return nil, errors.New("expired credentials require a new future expiry or never_expires=true")
		}
	}
	token, err := generateTenantAPIKeyToken()
	if err != nil {
		return nil, err
	}
	rotated, err := repo.RotateUserMCPAPIKey(ctx, userID, id, current.KeyHash, hashTenantAPIKey(token), tenantAPIKeyTokenHint(token), expiresAt)
	if err != nil {
		return nil, err
	}
	return &interfaces.TenantAPIKeyCreateResult{APIKey: rotated, Token: token}, nil
}
func (s *tenantAPIKeyService) RevokeUserMCPAPIKey(ctx context.Context, userID string, id uint64) error {
	return s.repo.(interfaces.UserMCPAPIKeyRepository).RevokeUserMCPAPIKey(ctx, userID, id)
}

func (s *tenantAPIKeyService) AuthenticateAPIKey(ctx context.Context, token string) (*types.TenantAPIKey, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, apprepo.ErrTenantAPIKeyNotFound
	}
	key, err := s.repo.GetAPIKeyByHash(ctx, hashTenantAPIKey(token))
	if err != nil {
		return nil, err
	}
	if key.RevokedAt != nil {
		return nil, apprepo.ErrTenantAPIKeyNotFound
	}
	if key.ExpiresAt != nil && time.Now().UTC().After(key.ExpiresAt.UTC()) {
		return nil, apprepo.ErrTenantAPIKeyNotFound
	}
	return key, nil
}

func (s *tenantAPIKeyService) RecordAPIKeyUsed(keyID uint64) {
	s.touchAPIKeyLastUsedAsync(keyID)
}

// touchAPIKeyLastUsedAsync persists last_used_at at most once per key per
// apiKeyLastUsedMinInterval. The write runs in a detached goroutine so auth
// latency is not tied to an UPDATE on the hot path.
func (s *tenantAPIKeyService) touchAPIKeyLastUsedAsync(keyID uint64) {
	now := time.Now().UTC()
	if v, ok := s.lastUsedTouch.Load(keyID); ok {
		if now.Sub(v.(time.Time)) < apiKeyLastUsedMinInterval {
			return
		}
	}
	s.lastUsedTouch.Store(keyID, now)
	go func(id uint64, at time.Time) {
		if err := s.repo.UpdateAPIKeyLastUsed(context.Background(), id, at); err != nil {
			logger.Warnf(context.Background(),
				"failed to update tenant api key last_used_at (id=%d): %v", id, err)
			s.lastUsedTouch.Delete(id)
		}
	}(keyID, now)
}

func (s *tenantAPIKeyService) ListAPIKeys(ctx context.Context, tenantID uint64) ([]*types.TenantAPIKey, error) {
	return s.repo.ListAPIKeys(ctx, tenantID)
}

func (s *tenantAPIKeyService) ListPlatformAPIKeys(ctx context.Context) ([]*types.TenantAPIKey, error) {
	return s.repo.ListPlatformAPIKeys(ctx)
}

// UpdateAPIKey 按创建接口的相同语义更新租户 API Key 配置。
// scoped Key 需要至少一个能力；full-access Key 会清空细粒度能力和知识库范围。
func (s *tenantAPIKeyService) UpdateAPIKey(
	ctx context.Context, req interfaces.TenantAPIKeyUpdateRequest,
) (*types.TenantAPIKey, error) {
	if req.TenantID == 0 {
		return nil, errors.New("tenant_id is required")
	}
	if req.APIKeyID == 0 {
		return nil, errors.New("api_key_id is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	capabilities := types.NormalizeAPIKeyCapabilities(types.StringArray(req.Capabilities))
	if !req.FullAccess && len(capabilities) == 0 {
		return nil, errors.New("capabilities are required for scoped API keys")
	}
	expiresAt := req.ExpiresAt
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
	}
	key := &types.TenantAPIKey{
		Name:             name,
		FullAccess:       req.FullAccess,
		KnowledgeBaseIDs: normalizeAPIKeyIDs(req.KnowledgeBaseIDs),
		Capabilities:     capabilities,
		ExpiresAt:        expiresAt,
	}
	if key.FullAccess {
		key.KnowledgeBaseIDs = nil
		key.Capabilities = nil
	}
	return s.repo.UpdateAPIKey(ctx, req.TenantID, req.APIKeyID, key)
}

func (s *tenantAPIKeyService) RevokeAPIKey(ctx context.Context, tenantID uint64, id uint64) error {
	return s.repo.RevokeAPIKey(ctx, tenantID, id)
}

func (s *tenantAPIKeyService) RevokePlatformAPIKey(ctx context.Context, id uint64) error {
	return s.repo.RevokePlatformAPIKey(ctx, id)
}

func (s *tenantAPIKeyService) BackfillMissingKeyHashes(ctx context.Context) (int, error) {
	has, err := s.repo.HasKeysWithPlaceholderHash(ctx)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, nil
	}
	keys, err := s.repo.ListKeysWithPlaceholderHash(ctx)
	if err != nil {
		return 0, err
	}
	backfilled := 0
	for _, key := range keys {
		if key == nil || strings.TrimSpace(key.APIKey) == "" {
			continue
		}
		hash := hashTenantAPIKey(key.APIKey)
		if key.KeyHash == hash {
			continue
		}
		if err := s.repo.UpdateAPIKeyHash(ctx, key.ID, hash); err != nil {
			return backfilled, err
		}
		backfilled++
	}
	return backfilled, nil
}

func generateTenantAPIKeyToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sk-" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashTenantAPIKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tenantAPIKeyTokenHint(token string) string {
	token = strings.TrimSpace(token)
	if len(token) > 4 {
		token = token[len(token)-4:]
	}
	return "••••" + token
}

func normalizeMCPClientTypeStrict(clientType types.MCPClientType) (types.MCPClientType, error) {
	raw := strings.ToLower(strings.TrimSpace(string(clientType)))
	if raw == "" {
		return types.MCPClientGeneric, nil
	}
	normalized := types.NormalizeMCPClientType(clientType)
	if normalized == types.MCPClientGeneric && raw != string(types.MCPClientGeneric) {
		return "", errors.New("unsupported client_type")
	}
	return normalized, nil
}

func normalizeUserMCPCapabilities(in []string, useDefault bool) (types.StringArray, error) {
	if len(in) == 0 && useDefault {
		return types.StringArray{string(types.APIKeyCapabilityRetrieve), string(types.APIKeyCapabilityChat)}, nil
	}
	normalized := types.NormalizeAPIKeyCapabilities(types.StringArray(in))
	if len(normalized) != len(uniqueNonEmptyUserMCPCapabilities(in)) {
		return nil, errors.New("unsupported user_mcp capability")
	}
	for _, capability := range normalized {
		switch types.APIKeyCapability(capability) {
		case types.APIKeyCapabilityRetrieve, types.APIKeyCapabilityChat, types.APIKeyCapabilityReadAgents:
		default:
			return nil, errors.New("unsupported user_mcp capability")
		}
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one capability is required")
	}
	return normalized, nil
}

func uniqueNonEmptyUserMCPCapabilities(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range in {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func normalizeAPIKeyIDs(in []string) types.StringArray {
	out := types.StringArray{}
	seen := map[string]struct{}{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
