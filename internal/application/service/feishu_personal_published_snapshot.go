package service

import (
	"context"
	"crypto/md5" // Existing local file identity; SHA256 remains authoritative.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// prepareExactPublishedSnapshot verifies a persisted KnowHub asset. Source
// credentials and live Feishu resources are deliberately absent from this path.
// It issues no execution nonce, lease, write scope or source authorization.
func (p *PersonalRecovery) prepareExactPublishedSnapshot(
	ctx context.Context, receipt types.PersonalExactReceipt,
) (snapshot *personalExactDocument, refusal error) {
	defer func() {
		if errors.Is(refusal, gorm.ErrRecordNotFound) || errors.Is(refusal, repository.ErrPersonalIdentity) || errors.Is(refusal, repository.ErrPersonalFence) {
			refusal = errors.Join(errPersonalExactDocument, refusal)
		}
	}()
	if p == nil || p.service.db.Name() != "postgres" || receipt.Version != 1 {
		return nil, errPersonalExactDocument
	}
	m, r, a, err := p.repo.Load(ctx, receipt.AttemptID)
	if err != nil {
		return nil, err
	}
	if types.FeishuAuthMode(m.AuthMode) != types.FeishuUserOAuth ||
		m.ID != receipt.ImportID || m.KBID != receipt.KnowledgeBaseID ||
		m.KnowledgeID != receipt.KnowledgeID || a.ImportID != m.ID || r.ImportID != m.ID ||
		a.LogicalKnowledgeID != m.KnowledgeID || r.LogicalKnowledgeID != m.KnowledgeID ||
		a.CallerTenantID != receipt.TenantID || r.CurrentAttemptID != a.ID ||
		a.Status != "indexed" || a.OwnerNonce != "" || a.LeaseUntil != nil ||
		r.Generation != a.Fence || r.Generation != receipt.Generation ||
		r.ActiveVersionID != a.CandidateVersionID || r.CandidateVersionID != r.ActiveVersionID ||
		a.CandidateVersionID == "" || a.CandidateVersionID == a.LogicalKnowledgeID ||
		a.ExpectedActiveVersion != receipt.ExpectedActive {
		return nil, errPersonalExactDocument
	}
	manifest, err := p.store.read(a)
	if err != nil {
		return nil, errors.Join(errPersonalExactDocument, err)
	}
	if manifest.Hash != a.ManifestHash || manifest.Size != a.ManifestSize ||
		manifest.Name != a.ManifestName || len(manifest.Assets) != 0 ||
		manifest.Size <= 0 || manifest.Size > 64*1024*1024 {
		return nil, errPersonalExactDocument
	}
	k, err := p.loadDispatchMetadata(ctx, receipt.TenantID, m.KBID, m.KnowledgeID)
	if err != nil {
		return nil, err
	}
	handle, ok := types.ParseResourcePath(k.FilePath)
	if !ok || k.Channel != "feishu_personal" || k.AttemptID != a.ID ||
		k.ManifestSHA256 != a.ManifestHash || k.FileSize != manifest.Size || k.FileName != manifest.Name {
		return nil, errPersonalExactDocument
	}
	var file types.StoredResource
	if err = p.service.db.WithContext(ctx).Where(
		"id=? AND handle=? AND tenant_id=? AND provider='local' AND state='active' AND deleted_at IS NULL",
		receipt.ResourceID, handle, receipt.TenantID).Take(&file).Error; err != nil {
		return nil, err
	}
	if file.Size != manifest.Size || file.Lifecycle != types.ResourceLifecyclePersistent ||
		file.ExpiresAt != nil || (strings.Contains(file.PhysicalPath, "://") && !strings.HasPrefix(file.PhysicalPath, "local://")) ||
		(file.ContentHash != "" && len(file.ContentHash) != 64) {
		return nil, errPersonalExactDocument
	}
	var bindings []types.ResourceBinding
	if err = p.service.db.WithContext(ctx).Where("resource_id=?", file.ID).Limit(2).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].ID != receipt.BindingID ||
		bindings[0].TenantID != receipt.TenantID || bindings[0].OwnerID != m.KnowledgeID ||
		bindings[0].OwnerType != types.ResourceOwnerKnowledge || bindings[0].Relation != types.ResourceRelationSourceFile {
		return nil, errPersonalExactDocument
	}
	ks, ok := p.service.knowledge.(*knowledgeService)
	if !ok || ks.fileSvc == nil {
		return nil, errPersonalExactDocument
	}
	// The physical path comes only from the checked, persistent local registry.
	// The local provider confines it under its base; no importer token is used.
	reader, err := ks.fileSvc.GetFile(ctx, file.PhysicalPath)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.Join(errPersonalExactDocument, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, file.Size+1))
	if err = errors.Join(readErr, reader.Close()); err != nil {
		return nil, err
	}
	sha, md := sha256.Sum256(data), md5.Sum(data)
	fileHash := hex.EncodeToString(sha[:])
	if int64(len(data)) != file.Size || (file.ContentHash != "" && fileHash != file.ContentHash) ||
		personalManifestDigest(data, nil) != manifest.Hash || hex.EncodeToString(md[:]) != k.FileHash {
		return nil, errPersonalExactDocument
	}
	proof := &personalExactDocument{
		knowledge: m.KnowledgeID, kb: m.KBID, tenant: a.CallerTenantID,
		importID: m.ID, attempt: a.ID, candidate: a.CandidateVersionID,
		expectedActive: a.ExpectedActiveVersion, resource: file.ID, resourceHandle: handle,
		manifestSHA256: manifest.Hash, fileSHA256: fileHash, grantEpoch: a.GrantEpoch,
		appVersion: a.AppConfigVersion, connectionIdentity: m.AppID + ":" + m.OpenID,
		revision: a.Revision, generation: a.Fence, file: data,
		fileMD5: k.FileHash, physicalPath: file.PhysicalPath, bindingID: bindings[0].ID, filename: manifest.Name,
	}
	if exactReceiptSourceIdentity(receipt) != exactSourceReceipt(proof) {
		return nil, errPersonalExactDocument
	}
	if err = p.verifyExactPublishedDatabase(ctx, receipt); err != nil {
		return nil, err
	}
	// Reject registry/binding changes during local file I/O as well. The outer
	// document read guard repeats the whole snapshot before releasing content.
	var currentFile types.StoredResource
	var currentBindings []types.ResourceBinding
	if err = p.service.db.WithContext(ctx).Where("id=?", file.ID).Take(&currentFile).Error; err != nil {
		return nil, err
	}
	if err = p.service.db.WithContext(ctx).Where("resource_id=?", file.ID).Limit(2).Find(&currentBindings).Error; err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(file, currentFile) || !reflect.DeepEqual(bindings, currentBindings) {
		return nil, errPersonalExactDocument
	}
	return proof, nil
}
