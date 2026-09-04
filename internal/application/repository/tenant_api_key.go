package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var ErrTenantAPIKeyNotFound = errors.New("tenant api key not found")

type tenantAPIKeyRepository struct {
	db *gorm.DB
}

func NewTenantAPIKeyRepository(db *gorm.DB) interfaces.TenantAPIKeyRepository {
	return &tenantAPIKeyRepository{db: db}
}

func (r *tenantAPIKeyRepository) CreateAPIKey(ctx context.Context, key *types.TenantAPIKey) error {
	if key.IsUserMCP() {
		scopes := key.TenantScopes
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			key.TenantScopes = nil
			if err := tx.Create(key).Error; err != nil {
				return err
			}
			for i := range scopes {
				scopes[i].APIKeyID = key.ID
				children := scopes[i].KnowledgeBases
				scopes[i].KnowledgeBases = nil
				if err := tx.Create(&scopes[i]).Error; err != nil {
					return err
				}
				for j := range children {
					children[j].APIKeyID = key.ID
					children[j].TenantID = scopes[i].TenantID
					children[j].APIKeyTenantScopeID = scopes[i].ID
				}
				if len(children) > 0 {
					if err := tx.Create(&children).Error; err != nil {
						return err
					}
				}
				scopes[i].KnowledgeBases = children
			}
			key.TenantScopes = scopes
			return nil
		})
	}
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *tenantAPIKeyRepository) GetAPIKeyByHash(ctx context.Context, hash string) (*types.TenantAPIKey, error) {
	var key types.TenantAPIKey
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Where("key_hash = ?", hash).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	if key.IsUserMCP() {
		if err := r.db.WithContext(ctx).Preload("KnowledgeBases").
			Where("api_key_id = ?", key.ID).Find(&key.TenantScopes).Error; err != nil {
			return nil, err
		}
	}
	return &key, nil
}

func (r *tenantAPIKeyRepository) ListUserMCPAPIKeys(ctx context.Context, userID string) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	err := r.db.WithContext(ctx).Preload("TenantScopes.KnowledgeBases").
		Where("scope_type = ? AND owner_user_id = ? AND revoked_at IS NULL", types.APIKeyScopeUserMCP, userID).
		Order("created_at DESC").Find(&keys).Error
	return keys, err
}

func (r *tenantAPIKeyRepository) GetUserMCPTenantScope(ctx context.Context, keyID, tenantID uint64) (*types.APIKeyTenantScope, error) {
	var scope types.APIKeyTenantScope
	err := r.db.WithContext(ctx).Preload("KnowledgeBases").Where("api_key_id=? AND tenant_id=?", keyID, tenantID).First(&scope).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	// A selected shared KB is intentionally pinned to one concrete share row.
	// Filter stale rows here so revoking and recreating a share never silently
	// restores an old key's access through another share path.
	if scope.KBScopeMode == types.APIKeyKBScopeSelected {
		active := make([]types.APIKeyKnowledgeBaseScope, 0, len(scope.KnowledgeBases))
		for _, kbScope := range scope.KnowledgeBases {
			if kbScope.SourceType != types.APIKeyKBSourceShared {
				active = append(active, kbScope)
				continue
			}
			if kbScope.KBShareID == nil {
				continue
			}
			var count int64
			queryErr := r.db.WithContext(ctx).Table("kb_shares AS ks").
				Joins("JOIN organization_tenant_members otm ON otm.organization_id = ks.organization_id AND otm.tenant_id = ?", tenantID).
				Joins("JOIN organizations o ON o.id = ks.organization_id AND o.deleted_at IS NULL").
				Joins("JOIN knowledge_bases kb ON kb.id = ks.knowledge_base_id AND kb.deleted_at IS NULL").
				Where("ks.id = ? AND ks.knowledge_base_id = ? AND ks.deleted_at IS NULL", *kbScope.KBShareID, kbScope.KnowledgeBaseID).
				Count(&count).Error
			if queryErr != nil {
				return nil, queryErr
			}
			if count > 0 {
				active = append(active, kbScope)
			}
		}
		scope.KnowledgeBases = active
	}
	return &scope, nil
}

func (r *tenantAPIKeyRepository) ReplaceUserMCPAPIKey(ctx context.Context, userID string, key *types.TenantAPIKey) (*types.TenantAPIKey, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&types.TenantAPIKey{}).Where("id=? AND owner_user_id=? AND scope_type=? AND revoked_at IS NULL", key.ID, userID, types.APIKeyScopeUserMCP).
			Updates(map[string]any{"name": key.Name, "expires_at": key.ExpiresAt})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrTenantAPIKeyNotFound
		}
		if err := tx.Where("api_key_id=?", key.ID).Delete(&types.APIKeyTenantScope{}).Error; err != nil {
			return err
		}
		for i := range key.TenantScopes {
			key.TenantScopes[i].APIKeyID = key.ID
			children := key.TenantScopes[i].KnowledgeBases
			key.TenantScopes[i].KnowledgeBases = nil
			if err := tx.Create(&key.TenantScopes[i]).Error; err != nil {
				return err
			}
			for j := range children {
				children[j].APIKeyID = key.ID
				children[j].TenantID = key.TenantScopes[i].TenantID
				children[j].APIKeyTenantScopeID = key.TenantScopes[i].ID
			}
			if len(children) > 0 {
				if err := tx.Create(&children).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, err := r.ListUserMCPAPIKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ID == key.ID {
			return row, nil
		}
	}
	return nil, ErrTenantAPIKeyNotFound
}

func (r *tenantAPIKeyRepository) RevokeUserMCPAPIKey(ctx context.Context, userID string, id uint64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&types.TenantAPIKey{}).
		Where("id=? AND owner_user_id=? AND scope_type=? AND revoked_at IS NULL", id, userID, types.APIKeyScopeUserMCP).Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantAPIKeyNotFound
	}
	return nil
}

func (r *tenantAPIKeyRepository) ListAPIKeys(ctx context.Context, tenantID uint64) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND revoked_at IS NULL", tenantID).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

func (r *tenantAPIKeyRepository) ListPlatformAPIKeys(ctx context.Context) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND revoked_at IS NULL", types.APIKeyScopePlatform).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

// UpdateAPIKey 更新租户 API Key 的可配置属性。
// tenant_id 和 scope_type 同时参与条件，避免跨租户或误改平台级 Key。
func (r *tenantAPIKeyRepository) UpdateAPIKey(
	ctx context.Context, tenantID uint64, id uint64, update *types.TenantAPIKey,
) (*types.TenantAPIKey, error) {
	res := r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND tenant_id = ? AND scope_type = ? AND revoked_at IS NULL",
			id, tenantID, types.APIKeyScopeTenant).
		Updates(map[string]any{
			"name":               update.Name,
			"full_access":        update.FullAccess,
			"knowledge_base_ids": update.KnowledgeBaseIDs,
			"capabilities":       update.Capabilities,
			"expires_at":         update.ExpiresAt,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTenantAPIKeyNotFound
	}

	var updatedKey types.TenantAPIKey
	if err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND revoked_at IS NULL", id, tenantID).
		First(&updatedKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantAPIKeyNotFound
		}
		return nil, err
	}
	return &updatedKey, nil
}

func (r *tenantAPIKeyRepository) RevokeAPIKey(ctx context.Context, tenantID uint64, id uint64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND tenant_id = ? AND revoked_at IS NULL", id, tenantID).
		Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantAPIKeyNotFound
	}
	return nil
}

func (r *tenantAPIKeyRepository) RevokePlatformAPIKey(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND scope_type = ? AND revoked_at IS NULL", id, types.APIKeyScopePlatform).
		Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantAPIKeyNotFound
	}
	return nil
}

func (r *tenantAPIKeyRepository) UpdateAPIKeyHash(ctx context.Context, id uint64, hash string) error {
	return r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("key_hash", hash).Error
}

// placeholderKeyHashPrefix mirrors the value written by migration
// 000065_tenant_api_keys.up.sql ('migrated-tenant-' || id). Rows still
// carrying it have never been authenticated since the upgrade, so their
// key_hash is not the real SHA-256 of the API key yet.
const placeholderKeyHashPrefix = "migrated-tenant-"

func (r *tenantAPIKeyRepository) HasKeysWithPlaceholderHash(ctx context.Context) (bool, error) {
	var id uint64
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Model(&types.TenantAPIKey{}).
		Select("id").
		Where("key_hash LIKE ? AND revoked_at IS NULL", placeholderKeyHashPrefix+"%").
		Limit(1).
		Scan(&id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return id != 0, nil
}

func (r *tenantAPIKeyRepository) ListKeysWithPlaceholderHash(
	ctx context.Context,
) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	// AfterFind decrypts api_key, so callers get the plaintext token needed
	// to compute the real hash.
	err := r.db.WithContext(ctx).
		Where("key_hash LIKE ? AND revoked_at IS NULL", placeholderKeyHashPrefix+"%").
		Find(&keys).Error
	return keys, err
}

func (r *tenantAPIKeyRepository) UpdateAPIKeyLastUsed(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("last_used_at", &at).Error
}
