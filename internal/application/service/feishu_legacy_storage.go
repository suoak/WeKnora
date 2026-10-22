package service

import (
	"context"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Resolve through the existing storage resolver, including RC3's registered
// local backends and resource catalog. Never fall back after a resolver error.
func (s *knowledgeService) legacyCandidateStorage(ctx context.Context, kb *types.KnowledgeBase, store *repository.FeishuLegacyStore) (interfaces.FileService, string, string, error) {
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil || tenant.ID != kb.TenantID {
		return nil, "", "", ErrFeishuStagingRequired
	}
	provider := kb.GetStorageProvider()
	if provider == "" {
		provider = strings.TrimSpace(os.Getenv("STORAGE_TYPE"))
	}
	if provider == "" {
		provider = "local"
	}
	if s.storageResolver == nil {
		if provider != "local" || kb.StorageBackendID != nil || tenant.DefaultStorageBackendID != nil || s.fileSvc == nil {
			return nil, "", "", ErrFeishuStagingRequired
		}
		return s.fileSvc, "", "", nil
	}
	id := ""
	if kb.StorageBackendID != nil {
		id = *kb.StorageBackendID
	}
	backend, err := s.storageResolver.ResolveBackend(ctx, tenant, id, provider)
	if err != nil {
		return nil, "", "", err
	}
	stamp := ""
	if backend != nil {
		if backend.TenantID != tenant.ID || backend.Provider != "local" || backend.Status != types.StorageBackendStatusActive {
			return nil, "", "", ErrFeishuStagingRequired
		}
		id = backend.ID
		stamp, err = store.StorageStamp(ctx, tenant.ID, id)
		if err != nil {
			return nil, "", "", err
		}
	}
	svc, resolved, err := s.storageResolver.ResolveFileService(ctx, tenant, id, provider, strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR")))
	if err != nil {
		return nil, "", "", err
	}
	if svc == nil || resolved != "local" {
		return nil, "", "", ErrFeishuStagingRequired
	}
	return svc, id, stamp, nil
}

func (s *knowledgeService) legacyPreviousFileIsControlled(ctx context.Context, kb *types.KnowledgeBase, path string) bool {
	if path == "" {
		return true
	}
	if _, registered := types.ParseResourcePath(path); registered {
		if s.resourceCatalog == nil {
			return false
		}
		r, err := s.resourceCatalog.Resolve(ctx, path)
		if err != nil || r == nil || r.TenantID != kb.TenantID || r.Provider != "local" {
			return false
		}
		path = r.PhysicalPath
	}
	if id, inner, scoped := types.ParseStorageBackendPath(path); scoped {
		if s.storageResolver == nil {
			return false
		}
		tenant, _ := types.TenantInfoFromContext(ctx)
		b, err := s.storageResolver.ResolveBackend(ctx, tenant, id, "")
		if err != nil || b == nil || b.TenantID != kb.TenantID || b.Provider != "local" {
			return false
		}
		path = inner
	}
	path = strings.TrimPrefix(path, "local://")
	return path != "" && !strings.Contains(path, "://")
}
