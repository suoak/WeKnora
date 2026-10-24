package repository

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validateLegacyAssets(tx *gorm.DB, r *types.FeishuLegacyResource, a *types.FeishuLegacyAttempt,
	m *types.FeishuLegacyManifest, publish bool,
) error {
	assets := map[string]bool{}
	if len(m.Assets) > 1000 {
		return legacyFence("asset_count_invalid")
	}
	for _, asset := range m.Assets {
		hash, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(hash) != 32 || asset.Bytes <= 0 || asset.Bytes > 32<<20 || assets[asset.Path] {
			return legacyFence("asset_manifest_invalid")
		}
		assets[asset.Path] = true
		physical := asset.Path
		var file types.StoredResource
		registered := false
		if handle, ok := types.ParseResourcePath(asset.Path); ok {
			registered = true
			err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&file,
				"handle=? AND tenant_id=? AND state='active' AND deleted_at IS NULL", handle, r.TenantID).Error
			if err != nil {
				return err
			}
			if file.Provider != "local" || file.StorageBackendID != m.StorageBackendID {
				return legacyFence("asset_storage_changed")
			}
			physical = file.PhysicalPath
		}
		if backend, inner, ok := types.ParseStorageBackendPath(physical); ok {
			if backend != m.StorageBackendID {
				return legacyFence("asset_storage_changed")
			}
			physical = inner
		}
		physical = strings.TrimPrefix(physical, "local://")
		ownerDirectory := "/" + strconv.FormatUint(r.TenantID, 10) + "/" + a.ID + "/"
		if strings.Contains(physical, "://") || strings.Contains(physical, "\\") ||
			path.Clean(physical) != physical || !strings.Contains("/"+physical, ownerDirectory) {
			return legacyFence("asset_attempt_mismatch")
		}
		if publish && registered {
			binding := &types.ResourceBinding{
				ResourceID: file.ID, TenantID: r.TenantID, OwnerType: types.ResourceOwnerKnowledge,
				OwnerID: r.KnowledgeID, Relation: types.ResourceRelationExtractedImage,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(binding).Error; err != nil {
				return err
			}
		}
	}
	used := map[string]bool{}
	for _, c := range m.Chunks {
		if c == nil {
			return legacyFence("invalid_chunk_identity")
		}
		for ref := range searchutil.ImageURLsInContent(c.Content) {
			if !assets[ref] {
				return legacyFence("unowned_image_reference")
			}
			used[ref] = true
		}
		if c.ImageInfo != "" {
			var images []types.ImageInfo
			if err := json.Unmarshal([]byte(c.ImageInfo), &images); err != nil {
				return legacyFence("asset_image_info_invalid")
			}
			for _, img := range images {
				if !assets[img.URL] || img.OriginalURL != img.URL {
					return legacyFence("unowned_image_info")
				}
				used[img.URL] = true
			}
		}
	}
	if len(used) != len(assets) {
		return legacyFence("unreferenced_asset")
	}
	return nil
}

// CheckpointAssets records verified uploads before expensive enrichment. It
// never binds to a public knowledge, advances the active pointer or renews an
// expired owner. The retained manifest also makes failed-attempt inventory
// possible without guessing filenames.
func (s *FeishuLegacyStore) CheckpointAssets(ctx context.Context, ds *types.DataSource,
	owner *types.FeishuLegacyAttempt, assets []types.FeishuLegacyAsset, storageID, storageStamp string,
) error {
	if len(assets) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, a, err := s.owned(ctx, tx, ds, owner)
		if err != nil {
			return err
		}
		if a.Status != "staging" {
			return legacyFence("attempt_not_staging")
		}
		var m types.FeishuLegacyManifest
		if err = json.Unmarshal(a.Manifest, &m); err != nil {
			return err
		}
		h, err := LegacyManifestHash(m)
		if err != nil || ((m.SourceIdentity != nil || m.SHA256 != "") && h != m.SHA256) {
			return legacyFence("manifest_integrity_mismatch")
		}
		// Validate ownership using temporary references, without creating any
		// real candidate/public chunk rows or changing parser output.
		validation := m
		validation.StorageBackendID = storageID
		validation.StorageStamp = storageStamp
		validation.Assets = assets
		validation.Chunks = nil
		for _, asset := range assets {
			encoded, _ := json.Marshal([]types.ImageInfo{{URL: asset.Path, OriginalURL: asset.Path}})
			validation.Chunks = append(validation.Chunks, &types.Chunk{ImageInfo: string(encoded)})
		}
		if err = validateLegacyAssets(tx, r, a, &validation, false); err != nil {
			return err
		}
		m.Assets = assets
		m.StorageBackendID = storageID
		m.StorageStamp = storageStamp
		m.SHA256, err = LegacyManifestHash(m)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return tx.Model(a).Update("manifest", json.RawMessage(raw)).Error
	})
}
