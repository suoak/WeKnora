package repository

import (
	"crypto/md5" // GitLab ingestion persists this legacy file identity.
	"crypto/sha256"
	"encoding/hex"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// A completed, empty plaintext GitLab file has no parser chunks. Admit only
// the tiny, exhaustively enumerable whitespace inputs whose legacy file hash
// identifies their exact bytes, with a unique persistent source-file binding.
// This cannot certify a nonempty document with missing parser output.
// The caller has already checked the current GitLab tenant/KB/source binding;
// its configuration and this evidence remain in every generation version.
func emptyGitLabWikiInputVersion(db *gorm.DB, tenant uint64, kb, id string) (string, error) {
	var doc struct {
		FilePath, FileHash, FileType string
		FileSize                     int64
	}
	if err := db.Table("knowledges").Select("file_path,file_hash,file_type,file_size").
		Where(`id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL
 AND channel='gitlab' AND type='file' AND enable_status='enabled'
 AND parse_status='completed' AND pending_subtasks_count=0`, id, tenant, kb).Take(&doc).Error; err != nil {
		return "", ErrWikiMCPUnverified
	}
	if doc.FileType != "md" && doc.FileType != "markdown" && doc.FileType != "txt" {
		return "", ErrWikiMCPUnverified
	}
	handle, ok := types.ParseResourcePath(doc.FilePath)
	if !ok || doc.FileSize < 0 || doc.FileSize > 2 {
		return "", ErrWikiMCPUnverified
	}
	var exactSHA string
	whitespace := []byte{' ', '\t', '\r', '\n'}
	candidates := [][]byte{{}}
	for _, a := range whitespace {
		candidates = append(candidates, []byte{a})
		for _, b := range whitespace {
			candidates = append(candidates, []byte{a, b})
		}
	}
	for _, candidate := range candidates {
		legacy := md5.Sum(candidate)
		if int64(len(candidate)) == doc.FileSize && hex.EncodeToString(legacy[:]) == doc.FileHash {
			strong := sha256.Sum256(candidate)
			exactSHA = hex.EncodeToString(strong[:])
			break
		}
	}
	if exactSHA == "" {
		return "", ErrWikiMCPUnverified
	}
	var chunks int64
	if err := db.Table("chunks").Where("knowledge_id=?", id).Count(&chunks).Error; err != nil {
		return "", err
	}
	if chunks != 0 {
		return "", ErrWikiMCPUnverified
	}
	var resource types.StoredResource
	if err := db.Where(`handle=? AND tenant_id=? AND provider='local' AND state='active'
 AND lifecycle='persistent' AND expires_at IS NULL AND deleted_at IS NULL`, handle, tenant).
		Take(&resource).Error; err != nil {
		return "", ErrWikiMCPUnverified
	}
	if resource.Size != doc.FileSize || resource.ContentHash != "" && resource.ContentHash != exactSHA {
		return "", ErrWikiMCPUnverified
	}
	var bindings []types.ResourceBinding
	if err := db.Where("resource_id=?", resource.ID).Limit(2).Find(&bindings).Error; err != nil {
		return "", err
	}
	if len(bindings) != 1 || bindings[0].TenantID != tenant || bindings[0].OwnerID != id ||
		bindings[0].OwnerType != types.ResourceOwnerKnowledge ||
		bindings[0].Relation != types.ResourceRelationSourceFile {
		return "", ErrWikiMCPUnverified
	}
	// Exclude timestamps from authority; changing the registered location, hash,
	// ownership or bytes changes the proof. No empty-file body enters a prompt.
	return wikiDigest([]any{
		doc, exactSHA, resource.ID, resource.Handle, resource.StorageBackendID,
		resource.Provider, resource.PhysicalPath, resource.LocationHash, resource.Size, resource.ContentHash,
		bindings[0].ID, bindings[0].TenantID, bindings[0].OwnerType, bindings[0].OwnerID, bindings[0].Relation,
	}), nil
}
