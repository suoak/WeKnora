package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/feishudocx"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const legacyDOCXIdentityAlgorithm = "feishu-docx-package-v1+name-title-v1"

// LegacySourceHash preserves the historical raw bytes, filename and title contract.
func LegacySourceHash(content []byte, name, title string) string {
	b, _ := json.Marshal([]any{content, name, title})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// LegacyDOCXSourceIdentity includes the complete canonical package, filename and title.
func LegacyDOCXSourceIdentity(content []byte, name, title string) (*types.FeishuLegacySourceIdentity, error) {
	digest, algorithm, err := feishudocx.CanonicalDOCXDigest(content)
	if err != nil {
		return nil, err
	}
	if algorithm != "feishu-docx-package-v1" {
		return nil, legacyFence("source_identity_algorithm_unconfirmed")
	}
	b, _ := json.Marshal([]any{digest, name, title})
	h := sha256.Sum256(b)
	return &types.FeishuLegacySourceIdentity{Algorithm: legacyDOCXIdentityAlgorithm, Digest: hex.EncodeToString(h[:])}, nil
}

// LegacySourceIdentityBaseline is a snapshot of a published raw-contract source.
// Its private fields cannot be constructed by HTTP or connector metadata.
type LegacySourceIdentityBaseline struct {
	resource types.FeishuLegacyResource
	attempt  types.FeishuLegacyAttempt
	manifest types.FeishuLegacyManifest
}

// FilePath is the original published archive, never a URL supplied by the new fetch.
func (b *LegacySourceIdentityBaseline) FilePath() string { return b.manifest.Knowledge.FilePath }

// LegacySourceIdentityProof can only be built after the archived raw hash is verified.
type LegacySourceIdentityProof struct {
	baseline *LegacySourceIdentityBaseline
	identity *types.FeishuLegacySourceIdentity
}

// ProveDOCX verifies the historical raw contract before computing package equivalence.
func (b *LegacySourceIdentityBaseline) ProveDOCX(content []byte) (*LegacySourceIdentityProof, error) {
	k := b.manifest.Knowledge
	metadata := k.GetMetadata()
	if metadata["obj_type"] != "docx" || metadata["attachment"] == "true" {
		return nil, legacyFence("source_identity_legacy_path_unconfirmed")
	}
	if LegacySourceHash(content, k.FileName, k.Title) != b.attempt.SourceHash {
		return nil, legacyFence("source_identity_legacy_archive_hash_mismatch")
	}
	identity, err := LegacyDOCXSourceIdentity(content, k.FileName, k.Title)
	if err != nil {
		return nil, err
	}
	return &LegacySourceIdentityProof{baseline: b, identity: identity}, nil
}

// SourceIdentityBaseline only exposes the proven published version for a controlled file read.
func (s *FeishuLegacyStore) SourceIdentityBaseline(
	ctx context.Context, ds *types.DataSource, externalID string,
) (*LegacySourceIdentityBaseline, error) {
	b := &LegacySourceIdentityBaseline{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(tx, ctx, ds); err != nil {
			return err
		}
		if err := tx.First(&b.resource, "id=?", legacyResourceID(ds, externalID)).Error; err != nil {
			return err
		}
		if b.resource.State != "active" || b.resource.ActiveAttempt == "" {
			return legacyFence("source_identity_legacy_archive_unavailable")
		}
		if b.resource.DataSourceID != ds.ID || b.resource.TenantID != ds.TenantID ||
			b.resource.KnowledgeBaseID != ds.KnowledgeBaseID || b.resource.ExternalID != externalID {
			return legacyFence("source_identity_legacy_manifest_unconfirmed")
		}
		if err := tx.First(&b.attempt, "id=? AND resource_id=? AND status='published'",
			b.resource.ActiveAttempt, b.resource.ID).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(b.attempt.Manifest, &b.manifest); err != nil {
			return err
		}
		hash, err := LegacyManifestHash(b.manifest)
		if err != nil || hash != b.manifest.SHA256 || b.manifest.SourceIdentity != nil {
			return legacyFence("source_identity_legacy_manifest_unconfirmed")
		}
		k := b.manifest.Knowledge
		if k.ID != b.resource.KnowledgeID || k.TenantID != ds.TenantID || k.KnowledgeBaseID != ds.KnowledgeBaseID ||
			k.FilePath == "" {
			return legacyFence("source_identity_legacy_archive_unavailable")
		}
		return nil
	})
	return b, err
}

func legacyIdentityValid(identity *types.FeishuLegacySourceIdentity) bool {
	if identity == nil {
		return true
	}
	_, err := hex.DecodeString(identity.Digest)
	return identity.Algorithm == legacyDOCXIdentityAlgorithm && len(identity.Digest) == 64 && err == nil
}

func legacyIdentityEqual(a, b *types.FeishuLegacySourceIdentity) bool {
	return a != nil && b != nil && *a == *b
}

func legacyAttemptSourceMatches(
	tx *gorm.DB, r *types.FeishuLegacyResource, a *types.FeishuLegacyAttempt,
	rawHash string, identity *types.FeishuLegacySourceIdentity, proof *LegacySourceIdentityProof,
) (bool, error) {
	if a.SourceHash == rawHash {
		return true, nil
	}
	var manifest types.FeishuLegacyManifest
	if err := json.Unmarshal(a.Manifest, &manifest); err != nil {
		return false, legacyFence("source_identity_manifest_unconfirmed")
	}
	if manifest.SourceIdentity != nil {
		hash, err := LegacyManifestHash(manifest)
		if err != nil || hash != manifest.SHA256 {
			return false, legacyFence("source_identity_manifest_unconfirmed")
		}
		return legacyIdentityEqual(manifest.SourceIdentity, identity), nil
	}
	if proof == nil || proof.baseline == nil || !legacyIdentityEqual(proof.identity, identity) {
		return false, nil
	}
	b := proof.baseline
	if a.ID != b.attempt.ID || a.ResourceID != b.resource.ID || a.Generation != b.attempt.Generation ||
		a.SourceHash != b.attempt.SourceHash || string(a.Manifest) != string(b.attempt.Manifest) ||
		r.ID != b.resource.ID || r.Generation != b.resource.Generation || r.SourceRevision != b.resource.SourceRevision ||
		r.ActiveAttempt != a.ID || r.State != "active" || a.Status != "published" {
		return false, legacyFence("source_identity_legacy_proof_stale")
	}
	var current types.Knowledge
	if err := tx.First(&current, "id=? AND tenant_id=?", r.KnowledgeID, r.TenantID).Error; err != nil {
		return false, err
	}
	if current.FilePath != b.manifest.Knowledge.FilePath || current.FileHash != b.manifest.Knowledge.FileHash ||
		current.KnowledgeBaseID != r.KnowledgeBaseID {
		return false, legacyFence("source_identity_legacy_archive_changed")
	}
	return true, nil
}
