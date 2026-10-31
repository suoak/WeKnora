package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// exactDocumentExecutionContext issues worker-only authority after the existing
// v3 owner has claimed the exact file. This is deliberately not a serving grant,
// model-input grant, KB read grant, or a value serialized in the task payload.
func (p *PersonalRecovery) exactDocumentExecutionContext(
	ctx context.Context, proof *personalExactDocument,
) (context.Context, error) {
	fresh, err := p.revalidateExactPersonalDocument(ctx, proof)
	if err != nil {
		return ctx, err
	}
	if err = p.exactExecutionReadable(ctx, fresh); err != nil {
		return ctx, err
	}
	read := func(current context.Context) error {
		tenant, ok := types.TenantIDFromContext(current)
		if !ok || tenant != fresh.tenant {
			return repository.ErrPersonalIdentity
		}
		if _, err := p.revalidateExactPersonalDocument(current, fresh); err != nil {
			return err
		}
		return p.exactExecutionReadable(current, fresh)
	}
	ctx = types.WithExecutionTenant(ctx, fresh.tenant)
	return types.WithExactDocumentScope(ctx, types.ExactDocumentScope{
		TenantID: fresh.tenant, KnowledgeID: fresh.knowledge, KBID: fresh.kb,
		Read: read,
		Commit: func(tx *gorm.DB) error {
			return p.commitExactPersonalDocument(tx, fresh)
		},
	})
}

// Reuse the canonical principal/grant/generation/nonce locks inside the DATA
// transaction. A check in a different transaction would leave a revocation or
// newer generation free to commit before the chunk/vector write.
func (p *PersonalRecovery) commitExactPersonalDocument(tx *gorm.DB, proof *personalExactDocument) error {
	if proof == nil || tx == nil || tx.Name() != "postgres" {
		return errPersonalExactDocument
	}
	local := *p
	local.repo = p.repo.InTransaction(tx)
	owner := &types.FeishuPersonalAttempt{
		ID: proof.attempt, Fence: proof.generation, OwnerNonce: proof.nonce,
		CandidateVersionID: proof.candidate, ExpectedActiveVersion: proof.expectedActive,
	}
	return local.mutate(tx.Statement.Context, owner, 0, func(locked *gorm.DB, a *types.FeishuPersonalAttempt) error {
		if a.ManifestHash != proof.manifestSHA256 || a.GrantEpoch != proof.grantEpoch ||
			a.AppConfigVersion != proof.appVersion || a.Revision != proof.revision ||
			a.ImportID != proof.importID || a.LogicalKnowledgeID != proof.knowledge ||
			a.CallerTenantID != proof.tenant {
			return repository.ErrPersonalFence
		}
		k, err := loadPersonalDispatchMetadata(locked, proof.tenant, proof.kb, proof.knowledge, true)
		if err != nil {
			return err
		}
		if k.Channel != "feishu_personal" || k.FileName != a.ManifestName ||
			k.AttemptID != proof.attempt || k.ManifestSHA256 != proof.manifestSHA256 ||
			k.FilePath != types.BuildResourcePath(proof.resourceHandle) || k.FileHash != proof.fileMD5 ||
			k.FileSize != int64(len(proof.file)) || k.ParseStatus == types.ParseStatusCancelled ||
			k.ParseStatus == types.ParseStatusDeleting {
			return repository.ErrPersonalIdentity
		}
		var resource types.StoredResource
		if err = locked.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"id=? AND tenant_id=? AND deleted_at IS NULL", proof.resource, proof.tenant,
		).First(&resource).Error; err != nil {
			return err
		}
		if resource.Handle != proof.resourceHandle || resource.Provider != "local" ||
			resource.PhysicalPath != proof.physicalPath ||
			resource.State != "active" || resource.Lifecycle != types.ResourceLifecyclePersistent ||
			resource.ExpiresAt != nil || resource.Size != int64(len(proof.file)) ||
			(resource.ContentHash != "" && resource.ContentHash != proof.fileSHA256) {
			return errPersonalExactDocument
		}
		var bindings []types.ResourceBinding
		if err = locked.Clauses(clause.Locking{Strength: "SHARE"}).Where("resource_id=?", resource.ID).
			Limit(2).Find(&bindings).Error; err != nil {
			return err
		}
		if len(bindings) != 1 || bindings[0].ID != proof.bindingID || bindings[0].TenantID != proof.tenant ||
			bindings[0].OwnerType != types.ResourceOwnerKnowledge ||
			bindings[0].OwnerID != proof.knowledge || bindings[0].Relation != types.ResourceRelationSourceFile {
			return errPersonalExactDocument
		}
		return nil
	})
}
