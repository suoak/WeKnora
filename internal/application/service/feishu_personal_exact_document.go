package service

import (
	"context"
	"crypto/md5" // Existing Knowledge identity uses MD5; authority uses SHA256.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errPersonalExactDocument = errors.New("personal_exact_document_unverified")

// A source attestation is not a KB grant, a model-input receipt or a serving
// receipt. Keep it private and non-serializable: task payloads cannot supply it.
// Consumers must fence publication and prove their actual derived inputs.
type personalExactDocument struct {
	knowledge, kb, importID, attempt, candidate, expectedActive string
	resource, resourceHandle, manifestSHA256, fileSHA256        string
	grantEpoch, appVersion, connectionIdentity, revision        string
	fileMD5, physicalPath, bindingID                            string
	tenant                                                      uint64
	generation                                                  int64
	nonce                                                       string
	leaseUntil                                                  time.Time
	file                                                        []byte
}

// Prepare only the v3 OAuth first-create source. Source acquisition and file
// hashing occur outside SQL locks. The final existing mutate transaction
// revalidates the owner, generation, lease and local authority before issuance.
// Nothing here disables a read hook or a database write fence.
func (p *PersonalRecovery) prepareExactPersonalDocument(
	ctx context.Context, owner *types.FeishuPersonalAttempt,
) (*personalExactDocument, error) {
	return p.prepareExactPersonalSource(ctx, owner, false)
}

// A published source is re-proved from the authoritative protocol; a consumed
// claim is never converted back into an execution owner. Serving requires a
// separate viewer grant and output receipt in addition to this source proof.
func (p *PersonalRecovery) prepareExactPersonalSource(
	ctx context.Context, owner *types.FeishuPersonalAttempt, published bool,
) (*personalExactDocument, error) {
	return p.checkExactPersonalSource(ctx, owner, published, nil)
}

type personalExactPreflight struct {
	observed *types.FeishuPersonalAttempt
	validate func(*gorm.DB, *types.FeishuPersonalAttempt) error
}

// Preflight reuses every source check without creating an execution/serving
// capability, receipt, lease or nonce. Refusal leaves the pending protocol intact.
func (p *PersonalRecovery) preflightExactPersonalDocument(
	ctx context.Context, observed *types.FeishuPersonalAttempt,
) (*personalExactPreflight, error) {
	preflight := &personalExactPreflight{observed: observed}
	_, err := p.checkExactPersonalSource(ctx, observed, false, preflight)
	if err != nil {
		return nil, err
	}
	return preflight, nil
}

func (p *PersonalRecovery) checkExactPersonalSource(
	ctx context.Context, owner *types.FeishuPersonalAttempt, published bool, check *personalExactPreflight,
) (proof *personalExactDocument, refusal error) {
	preflight := check != nil
	stage := "preconditions"
	var permissionBits *[3]bool
	defer func() {
		if preflight && refusal != nil && owner != nil && exactPersonalRecoveryAllows(owner.LogicalKnowledgeID) {
			logExactPersonalPreflightRefusal(ctx, owner, stage, permissionBits, refusal)
		}
	}()
	if owner == nil || !p.firstCreateOnly || p.service.db.Name() != "postgres" {
		return nil, errPersonalExactDocument
	}
	if preflight && (owner.Status != "index_pending" || owner.OwnerNonce != "" || owner.LeaseUntil != nil) {
		return nil, errPersonalExactDocument
	}
	if !published && !preflight &&
		(owner.OwnerNonce == "" || owner.LeaseUntil == nil || !owner.LeaseUntil.After(time.Now())) {
		return nil, errPersonalExactDocument
	}
	if published && (owner.Status != "indexed" || owner.OwnerNonce != "" || owner.LeaseUntil != nil) {
		return nil, errPersonalExactDocument
	}
	stage = "protocol_identity"
	m, r, a, err := p.repo.Load(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	if types.FeishuAuthMode(m.AuthMode) != types.FeishuUserOAuth ||
		a.Fence != owner.Fence || r.Generation != owner.Fence ||
		r.CurrentAttemptID != owner.ID ||
		m.KnowledgeID != a.LogicalKnowledgeID || r.LogicalKnowledgeID != m.KnowledgeID ||
		a.CandidateVersionID == a.LogicalKnowledgeID || a.ExpectedActiveVersion != "" {
		return nil, errPersonalExactDocument
	}
	if preflight && (a.Status != "index_pending" || a.OwnerNonce != "" || a.LeaseUntil != nil ||
		r.ActiveVersionID != owner.ExpectedActiveVersion || a.CandidateVersionID != owner.CandidateVersionID) {
		return nil, errPersonalExactDocument
	}
	if !published && !preflight && (a.Status != "processing" || a.OwnerNonce != owner.OwnerNonce ||
		a.LeaseUntil == nil || !a.LeaseUntil.After(time.Now()) || r.ActiveVersionID != owner.ExpectedActiveVersion) {
		return nil, errPersonalExactDocument
	}
	if published && (a.Status != "indexed" || a.OwnerNonce != "" || a.LeaseUntil != nil ||
		r.ActiveVersionID != a.CandidateVersionID || a.CandidateVersionID != owner.CandidateVersionID) {
		return nil, errPersonalExactDocument
	}
	stage = "local_authority"
	remote, err := p.authorize(ctx, m, a)
	if err != nil {
		return nil, err
	}
	expected := core.PersonalResource{Type: m.ResourceType, Token: m.DocumentToken}
	stage = "remote_resource"
	current, err := p.resolveResource(remote, m, expected)
	if err != nil {
		return nil, err
	}
	stage = "remote_identity"
	if current.Token != expected.Token || current.Type != expected.Type {
		return nil, errPersonalExactDocument
	}
	stage = "remote_revision"
	if current.Modified != a.Revision {
		return nil, errPersonalExactDocument
	}
	client := p.service.client(&core.Config{AppID: m.AppID})
	stage = "remote_permissions"
	permission, err := client.InspectPersonalPermissions(remote, expected)
	if err != nil {
		return nil, err
	}
	permissionBits = &[3]bool{permission.View, permission.Export, permission.Copy}
	if !permission.View || !permission.Export || !permission.Copy {
		return nil, errPersonalExactDocument
	}
	stage = "manifest"
	manifest, err := p.store.read(a)
	if err != nil {
		return nil, err
	}
	if manifest.Hash != a.ManifestHash || manifest.Size != a.ManifestSize ||
		manifest.Name != a.ManifestName || len(manifest.Assets) != 0 ||
		manifest.Size <= 0 || manifest.Size > 64*1024*1024 {
		return nil, errPersonalExactDocument
	}
	stage = "knowledge_metadata"
	k, err := p.loadDispatchMetadata(ctx, a.CallerTenantID, m.KBID, m.KnowledgeID)
	if err != nil {
		return nil, err
	}
	handle, ok := types.ParseResourcePath(k.FilePath)
	if !ok || k.Channel != "feishu_personal" || k.AttemptID != a.ID || k.ManifestSHA256 != a.ManifestHash ||
		k.FileSize != manifest.Size || k.FileName != manifest.Name {
		return nil, errPersonalExactDocument
	}
	stage = "file_registration"
	var file types.StoredResource
	if err = p.service.db.WithContext(ctx).Where(
		"handle=? AND tenant_id=? AND provider='local' AND state='active' AND deleted_at IS NULL",
		handle, a.CallerTenantID).First(&file).Error; err != nil {
		return nil, err
	}
	if file.Size != manifest.Size || (file.ContentHash != "" && len(file.ContentHash) != 64) ||
		file.Lifecycle != types.ResourceLifecyclePersistent ||
		file.ExpiresAt != nil {
		return nil, errPersonalExactDocument
	}
	stage = "file_binding"
	var bindings []types.ResourceBinding
	if err = p.service.db.WithContext(ctx).Where("resource_id=?", file.ID).Limit(2).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].TenantID != a.CallerTenantID ||
		bindings[0].OwnerID != m.KnowledgeID || bindings[0].OwnerType != types.ResourceOwnerKnowledge ||
		bindings[0].Relation != types.ResourceRelationSourceFile {
		return nil, errPersonalExactDocument
	}
	// Include historical/soft-deleted ownership: deletion does not release old
	// cloud data from protection. Unknown or mixed dependencies remain closed.
	stage = "legacy_overlap"
	var overlaps bool
	err = p.service.db.WithContext(ctx).Raw(`SELECT
EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE tenant_id=? AND
 (knowledge_id=? OR original_file_path IN (?,?) OR external_id=? OR external_id=?)) OR
EXISTS(SELECT 1 FROM feishu_legacy_attempts la JOIN feishu_legacy_resources lr ON lr.id=la.resource_id
 WHERE lr.tenant_id=? AND la.manifest->'knowledge'->>'file_path' IN (?,?)) OR
EXISTS(SELECT 1 FROM data_sources WHERE tenant_id=? AND type IN ('feishu','feishu_drive','lark','lark_drive')
 AND position(? in config::text)>0)`, a.CallerTenantID, m.KnowledgeID, k.FilePath, file.PhysicalPath,
		m.DocumentToken, m.ResourceType+":"+m.DocumentToken, a.CallerTenantID,
		k.FilePath, file.PhysicalPath, a.CallerTenantID, m.DocumentToken).Scan(&overlaps).Error
	if err != nil {
		return nil, err
	}
	if overlaps || m.DocumentToken == "" || p.service.knowledge.(*knowledgeService).fileSvc == nil {
		return nil, errPersonalExactDocument
	}
	// Hold the actual checked bytes, rather than authorizing a path which may
	// later refer to different content. Parser integration must consume these
	// bytes and prove media/embedded dependencies separately.
	stage = "file_read"
	reader, err := p.service.knowledge.(*knowledgeService).fileSvc.GetFile(
		remote,
		file.PhysicalPath,
	)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, file.Size+1))
	if err = errors.Join(readErr, reader.Close()); err != nil {
		return nil, err
	}
	stage = "file_digest"
	sum := sha256.Sum256(data)
	fileHash := hex.EncodeToString(sum[:])
	if int64(len(data)) != file.Size || (file.ContentHash != "" && fileHash != file.ContentHash) ||
		personalManifestDigest(data, nil) != manifest.Hash {
		return nil, errPersonalExactDocument
	}
	md5sum := md5.Sum(data)
	if hex.EncodeToString(md5sum[:]) != k.FileHash {
		return nil, errPersonalExactDocument
	}
	var issued *personalExactDocument
	verify := func(tx *gorm.DB, locked *types.FeishuPersonalAttempt) error {
		if locked.ManifestHash != manifest.Hash ||
			locked.CandidateVersionID != a.CandidateVersionID ||
			locked.Revision != a.Revision ||
			locked.GrantEpoch != a.GrantEpoch {
			return repository.ErrPersonalFence
		}
		bound, err := loadPersonalDispatchMetadata(
			tx,
			a.CallerTenantID,
			m.KBID,
			m.KnowledgeID,
			true,
		)
		if err != nil {
			return err
		}
		if *bound != *k {
			return repository.ErrPersonalIdentity
		}
		var lockedFile types.StoredResource
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", file.ID).
			First(&lockedFile).Error; err != nil {
			return err
		}
		if lockedFile.Handle != file.Handle || lockedFile.Provider != file.Provider ||
			lockedFile.TenantID != file.TenantID || lockedFile.PhysicalPath != file.PhysicalPath ||
			lockedFile.ContentHash != file.ContentHash || lockedFile.Size != file.Size ||
			lockedFile.LocationHash != file.LocationHash || lockedFile.StorageBackendID != file.StorageBackendID ||
			lockedFile.State != "active" || lockedFile.DeletedAt.Valid ||
			lockedFile.ExpiresAt != nil || lockedFile.Lifecycle != types.ResourceLifecyclePersistent {
			return errPersonalExactDocument
		}
		var lockedBindings []types.ResourceBinding
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("resource_id=?", file.ID).
			Limit(2).Find(&lockedBindings).Error; err != nil {
			return err
		}
		if len(lockedBindings) != 1 || lockedBindings[0] != bindings[0] {
			return errPersonalExactDocument
		}
		// Recheck registry/binding identity in the existing fenced transaction.
		var n int64
		if err := tx.Table("resources AS f").Joins("JOIN resource_bindings b ON b.resource_id=f.id").
			Where(`f.id=? AND f.handle=? AND f.tenant_id=? AND f.state='active' AND f.deleted_at IS NULL
AND f.content_hash=? AND f.size=? AND f.physical_path=? AND b.id=?
AND b.tenant_id=? AND b.owner_type='knowledge' AND b.owner_id=? AND b.relation='source_file'
AND (SELECT count(*) FROM resource_bindings all_b WHERE all_b.resource_id=f.id)=1`,
				file.ID, handle, a.CallerTenantID, file.ContentHash, file.Size, file.PhysicalPath,
				bindings[0].ID, a.CallerTenantID, m.KnowledgeID).Count(&n).Error; err != nil {
			return err
		}
		if n != 1 {
			return errPersonalExactDocument
		}
		if preflight {
			return nil
		}
		var leaseUntil time.Time
		if locked.LeaseUntil != nil {
			leaseUntil = *locked.LeaseUntil
		}
		issued = &personalExactDocument{
			knowledge: m.KnowledgeID, kb: m.KBID, tenant: a.CallerTenantID,
			importID: m.ID, attempt: a.ID, candidate: a.CandidateVersionID,
			expectedActive: a.ExpectedActiveVersion, resource: file.ID, resourceHandle: handle,
			manifestSHA256: manifest.Hash, fileSHA256: fileHash, grantEpoch: a.GrantEpoch,
			appVersion: a.AppConfigVersion, connectionIdentity: m.AppID + ":" + m.OpenID,
			revision: a.Revision, generation: owner.Fence, nonce: owner.OwnerNonce,
			leaseUntil: leaseUntil, file: data,
			fileMD5: k.FileHash, physicalPath: file.PhysicalPath, bindingID: bindings[0].ID,
		}
		return nil
	}
	stage = "locked_local_snapshot"
	if !published && !preflight {
		err = p.mutate(ctx, owner, 0, verify)
	} else {
		// Read-only identity locking, never Claim/Mutate and never a new lease.
		err = p.service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var resource types.FeishuPersonalResource
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("import_id=?", a.ImportID).
				Take(&resource).Error; err != nil {
				return err
			}
			var current types.FeishuPersonalAttempt
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", a.ID).
				Take(&current).Error; err != nil {
				return err
			}
			if resource.CurrentAttemptID != a.ID || resource.Generation != a.Fence ||
				resource.LogicalKnowledgeID != a.LogicalKnowledgeID ||
				resource.CandidateVersionID != a.CandidateVersionID ||
				current.CandidateVersionID != a.CandidateVersionID ||
				current.ExpectedActiveVersion != a.ExpectedActiveVersion ||
				current.OwnerNonce != "" || current.LeaseUntil != nil || current.Fence != a.Fence {
				return repository.ErrPersonalFence
			}
			if preflight {
				if resource.ActiveVersionID != a.ExpectedActiveVersion || current.Status != "index_pending" {
					return repository.ErrPersonalFence
				}
			} else if resource.ActiveVersionID != a.CandidateVersionID || current.Status != "indexed" {
				return repository.ErrPersonalFence
			}
			return verify(tx, &current)
		})
	}
	if err == nil && preflight {
		// Detect local revocation during remote/file inspection as a preflight
		// refusal. Claim repeats the locked authority checks transactionally.
		stage = "authority_recheck"
		if _, err = p.authorize(ctx, m, a); err != nil {
			return nil, err
		}
		check.validate = verify
	}
	return issued, err
}

// Claim and the existing local authority/binding checks commit together. Any
// stale preflight rolls back ownership as well as generation and lease changes.
func (p *PersonalRecovery) claimExactPreflight(
	ctx context.Context, preflight *personalExactPreflight,
) (*types.FeishuPersonalAttempt, error) {
	if preflight == nil || preflight.observed == nil || preflight.validate == nil {
		return nil, errPersonalExactDocument
	}
	return p.repo.ClaimObserved(ctx, preflight.observed, p.lease,
		func(tx *gorm.DB, owner *types.FeishuPersonalAttempt) error {
			transactionRecovery := *p
			transactionRecovery.repo = p.repo.InTransaction(tx)
			return transactionRecovery.mutate(ctx, owner, 0, preflight.validate)
		})
}
